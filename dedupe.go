package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	drive "google.golang.org/api/drive/v2"
	"google.golang.org/api/googleapi"
)

const fields = googleapi.Field("items(id,title,fileSize,md5Checksum),nextPageToken")

var tok = flag.String("tok", "", "OAuth token")

func main() {
	flag.Parse()

	svc, err := drive.New(&http.Client{Transport: authTransport(*tok)})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	pageToken := ""

	type file struct {
		md5, title string
		size       int64
	}

	scannedFiles := 0
	md5s := map[file][]string{}
	fmt.Print("scanning files")
	for {
		fmt.Print(".")
		fs, err := svc.Files.List().
			MaxResults(1000).
			PageToken(pageToken).
			Fields(fields).
			Do()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
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
		time.Sleep(time.Second)
	}

	var totalFiles int
	var totalSize int64
	for k, v := range md5s {
		if len(v) > 1 {
			fmt.Println("===", k.title, "(md5:", k.md5, ") ===")
			fmt.Println("- reapable IDs:", strings.Join(v[1:], ", "))
			totalFiles += len(v) - 1
			totalSize += k.size * int64(len(v)-1)
		}
	}
	fmt.Println("scanned", scannedFiles, "files")
	fmt.Println("can reap", totalFiles, "files")
	fmt.Println("can reclaim", float64(totalSize)/1024/1024/1024, "GiB of space")
}

type authTransport string

func (t authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("Authorization", "Bearer "+string(t))
	return http.DefaultTransport.RoundTrip(r)
}
