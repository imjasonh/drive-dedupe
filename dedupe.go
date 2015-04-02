package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	drive "google.golang.org/api/drive/v2"
	"google.golang.org/api/googleapi"
)

const fields = googleapi.Field("items(id,title,md5Checksum),nextPageToken")

var tok = flag.String("tok", "", "OAuth token")

func main() {
	flag.Parse()

	svc, err := drive.New(&http.Client{Transport: authTransport{*tok}})
	if err != nil {
		log.Fatal(err)
	}
	pageToken := ""

	type file struct {
		id, title string
	}

	md5s := map[string][]file{}
	for i := 0; i < 10; i++ {
		log.Printf("querying page token %q\n", pageToken)
		fs, err := svc.Files.List().
			MaxResults(1000).
			PageToken(pageToken).
			Fields(fields).
			Do()
		if err != nil {
			log.Fatal(err)
		}
		for _, f := range fs.Items {
			if f.Md5Checksum != "" {
				md5s[f.Md5Checksum] = append(md5s[f.Md5Checksum], file{f.Id, f.Title})
			}
		}
		pageToken = fs.NextPageToken
		if pageToken == "" {
			break
		}
		time.Sleep(time.Second)
	}

	for k, v := range md5s {
		if len(v) > 1 {
			log.Println("===", k, "===")
			for _, v := range v[1:] {
				log.Println(v.id, v.title)
			}
		}
	}
}

type authTransport struct {
	tok string
}

func (t authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("Authorization", "Bearer "+t.tok)
	return http.DefaultTransport.RoundTrip(r)
}
