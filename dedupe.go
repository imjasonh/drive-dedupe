package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	drive "google.golang.org/api/drive/v2"
	"google.golang.org/api/googleapi"

	"appengine"
	"appengine/delay"
	"appengine/mail"
	"appengine/urlfetch"
)

const (
	clientID        = "1045967131934-0gdt52c0bp0e1g9dquib9atfvqjmjkjl.apps.googleusercontent.com"
	clientSecret    = "Dna3ItrA-0yhbpvjj_8516oG"
	redirectURLPath = "/oauth"
	fields          = googleapi.Field("items(id,title,fileSize,md5Checksum),nextPageToken")
)

var scopes = strings.Join([]string{
	drive.DriveReadonlyScope,
	"email",
}, " ")

func init() {
	http.HandleFunc("/", startHandler)
	http.HandleFunc(redirectURLPath, oauthHandler)
}

func startHandler(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)

	redirectURL := fmt.Sprintf("https://%s.appspot.com", appengine.AppID(ctx)) + redirectURLPath
	url := fmt.Sprintf("https://accounts.google.com/o/oauth2/auth?response_type=code&approval_prompt=force&client_id=%s&redirect_uri=%s&scope=%s",
		clientID, redirectURL, scopes)
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
	fmt.Fprintf(w, "generating report...") // TODO: moar pretty!
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

	scannedFiles := 0
	md5s := map[file][]string{}
	for {
		fs, err := svc.Files.List().
			MaxResults(1000).
			PageToken(pageToken).
			Fields(fields).
			Do()
		if err != nil {
			return err
		}
		scannedFiles += len(fs.Items)
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

	var totalFiles int
	var totalSize int64
	for k, v := range md5s {
		if len(v) > 1 {
			//ctx.Infof("===", k.title, "(md5:", k.md5, ") ===")
			//ctx.Infof("- reapable IDs:", strings.Join(v[1:], ", "))
			totalFiles += len(v) - 1
			totalSize += k.size * int64(len(v)-1)
		}
	}

	ctx.Infof("sending report to %q", email)

	msg := fmt.Sprintf("scanned %d files, found %d reapable, totalling %d bytes", scannedFiles, totalFiles, totalSize)
	return mail.Send(ctx, &mail.Message{
		Sender:  "imjasonh@gmail.com",
		To:      []string{email},
		Subject: "Drive Deduplifier Report",
		Body:    msg,
	})
})

type authTransport struct {
	tok    string
	client *http.Client
}

func (t authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("Authorization", "Bearer "+t.tok)
	return t.client.Do(r)
}
