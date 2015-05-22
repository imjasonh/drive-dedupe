// TODO: Store the dedupe report in datastore, including the IDs of files we
// can trash, and send a link to initiate that process. It might make sense to
// also re-parent duplicate files so they end up in the same folders as their
// trashed duplicates, I'll have to test if that actually works. If we store an
// actionable dedupe report, it should expire after some amount of time (should
// it also contain creds? :-/) and the email should include a list of duplicate
// files that would be trashed.

package dedupe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	humanize "github.com/dustin/go-humanize"
	drive "google.golang.org/api/drive/v2"

	"appengine"
	"appengine/delay"
	"appengine/mail"
	"appengine/urlfetch"
)

const (
	clientID        = "1045967131934-0gdt52c0bp0e1g9dquib9atfvqjmjkjl.apps.googleusercontent.com"
	clientSecret    = "Dna3ItrA-0yhbpvjj_8516oG"
	redirectURLPath = "/oauth"
)

var scopes = strings.Join([]string{
	drive.DriveMetadataReadonlyScope,
	"email",
}, " ")

var emailTmpl = template.Must(template.New("email").Parse(`
<html><body>
<h1>You have {{.ReapableBytes}} of duplicate files</h1>

<p>The <b>Drive Deduplifier</b> has scanned {{.TotalFiles}} files and found {{.ReapableFiles}} that are duplicates.</p>

<p>You are using <b>{{.UsedQuota}}</b> of your total {{.TotalQuota}} quota.</p>
</body></html>
`))

func init() {
	http.HandleFunc("/start", startHandler)
	http.HandleFunc(redirectURLPath, oauthHandler)
}

func startHandler(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
	redirectURL := fmt.Sprintf("https://%s.appspot.com", appengine.AppID(ctx)) + redirectURLPath
	url := "https://accounts.google.com/o/oauth2/auth?" + url.Values{
		"response_type":   {"code"},
		"approval_prompt": {"force"},
		"client_id":       {clientID},
		"redirect_uri":    {redirectURL},
		"scope":           {scopes},
	}.Encode()
	http.Redirect(w, r, url, http.StatusSeeOther)
}

func oauthHandler(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
	code := r.FormValue("code")
	tok, err := getAccessToken(ctx, code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	generateReport.Call(ctx, tok)
	http.Redirect(w, r, "/started", http.StatusSeeOther)
}

func getAccessToken(ctx appengine.Context, code string) (string, error) {
	client := urlfetch.Client(ctx)

	req, err := http.NewRequest("POST", "https://accounts.google.com/o/oauth2/token", strings.NewReader(url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"scope":         {scopes},
		"redirect_uri":  {fmt.Sprintf("https://%s.appspot.com", appengine.AppID(ctx)) + redirectURLPath},
		"grant_type":    {"authorization_code"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		ctx.Errorf("exchanging code: %v", err)
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		all, _ := ioutil.ReadAll(resp.Body)
		ctx.Errorf(string(all))
		return "", errors.New("couldn't get token")
	}

	var b struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		ctx.Errorf("decoding json: %v", err)
		return "", err
	}
	return b.AccessToken, nil
}

func getEmail(ctx appengine.Context, tok string) (string, error) {
	client := &http.Client{Transport: authTransport{tok, urlfetch.Client(ctx)}}
	req, _ := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http error %d", resp.StatusCode)
	}
	var info struct {
		Email string `json:"email"`
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	return info.Email, nil
}

var generateReport = delay.Func("generate", func(ctx appengine.Context, tok string) error {
	email, err := getEmail(ctx, tok)
	if err != nil {
		return err
	}

	client := &http.Client{Transport: authTransport{tok, urlfetch.Client(ctx)}}
	svc, err := drive.New(client)
	if err != nil {
		return err
	}
	pageToken := ""

	type file struct {
		md5, title string
		size       int64
	}

	var report report
	md5s := map[file][]string{}
	for {
		fs, err := svc.Files.List().
			MaxResults(1000).
			PageToken(pageToken).
			Fields("items(id,title,fileSize,md5Checksum),nextPageToken").
			Do()
		if err != nil {
			return err
		}
		report.TotalFiles += len(fs.Items)
		for _, f := range fs.Items {
			if f.Md5Checksum != "" {
				k := file{f.Md5Checksum, f.Title, f.FileSize}
				md5s[k] = append(md5s[k], f.Id)
			}
		}
		pageToken = fs.NextPageToken
		if pageToken == "" {
			break
		}
	}

	for k, v := range md5s {
		if len(v) > 1 {
			//ctx.Infof("===", k.title, "(md5:", k.md5, ") ===")
			//ctx.Infof("- reapable IDs:", strings.Join(v[1:], ", "))
			report.ReapableFiles += len(v) - 1
			report.ReapableBytes += k.size * int64(len(v)-1)
		}
	}

	about, err := svc.About.Get().
		Fields("quotaBytesUsed,quotaBytesTotal").
		Do()
	if err != nil {
		return err
	}
	report.TotalQuota = about.QuotaBytesTotal
	report.UsedQuota = about.QuotaBytesUsed

	ctx.Infof("sending report to %q", email)

	var body bytes.Buffer
	if err := emailTmpl.Execute(&body, map[string]interface{}{
		"TotalFiles":    report.TotalFiles,
		"ReapableFiles": report.ReapableFiles,
		"ReapableBytes": humanize.Bytes(uint64(report.ReapableBytes)),
		"TotalQuota":    humanize.Bytes(uint64(report.TotalQuota)),
		"UsedQuota":     humanize.Bytes(uint64(report.UsedQuota)),
	}); err != nil {
		return err
	}
	return mail.Send(ctx, &mail.Message{
		Sender:   "imjasonh@gmail.com",
		To:       []string{email},
		Subject:  "Drive Deduplifier Report",
		HTMLBody: body.String(),
	})
})

type report struct {
	TotalFiles, ReapableFiles int
	ReapableBytes             int64
	TotalQuota, UsedQuota     int64
}

type authTransport struct {
	tok    string
	client *http.Client
}

func (t authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("Authorization", "Bearer "+t.tok)
	return t.client.Do(r)
}
