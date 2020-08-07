package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"github.com/gobuffalo/envy"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
)

const zoomRecodingsURL = "https://api.zoom.us/v2/users/%s/recordings"

type recordingListResponse struct {
	Meetings []struct {
		RecordingFiles []recordingFile `json:"recording_files"`
	} `json:"meetings"`
}

type recordingFile struct {
	RecordingStart string `json:"recording_start"`
	FileType       string `json:"file_type"`
	DownloadURL    string `json:"download_url"`
	RecordingType  string `json:"recording_type"`
	Status         string `json:"status"`
}

func main() {
	zoomJWT := envy.Get("ZOOM_JWT", "")
	if zoomJWT == "" {
		log.Fatal("Please set ZOOM_JWT to access the zoom API.")
	}

	zoomUserID := envy.Get("ZOOM_USER_ID", "")
	if zoomUserID == "" {
		log.Fatal("Please set ZOOM_USER_ID from which to retreive recording.")
	}

	bucket := envy.Get("GSTORAGE_BUCKET", "")
	if bucket == "" {
		log.Fatal("Please set GSTORAGE_BUCKET with a bucket as a backup destination")
	}

	gstoragePath := envy.Get("GSTORAGE_PATH", "")
	if gstoragePath == "" {
		log.Fatal("Please set GSTORAGE_PATH with a path as a backup destination")
	}

	credsJSON := envy.Get("GCLOUD_STORAGE_CREDS", "")
	if credsJSON == "" {
		log.Fatal("Please set GCLOUD_STORAGE_CREDS with your JSON creds from Google Cloud.")
	}

	ctx := context.Background()

	var err error
	creds, err := google.CredentialsFromJSON(ctx, []byte(credsJSON), storage.ScopeReadWrite)
	if err != nil {
		log.Fatal("Error parsing credential from JSON ", err)
	}

	storageClient, err := storage.NewClient(ctx, option.WithCredentials(creds))
	if err != nil {
		log.Fatal("Error creating new storage client ", err)
	}

	recordings, err := fetchRecordings(zoomJWT, zoomUserID)
	if err != nil {
		err = fmt.Errorf("failed to fetch recordings: %w", err)
		log.Fatal(err)
	}

	wg := sync.WaitGroup{}

	for _, recording := range recordings {
		wg.Add(1)
		go func(recording recordingFile) {
			fileName := recording.FileName()
			fmt.Println("Requesting", fileName)
			body, err := requestRecordingFile(recording.DownloadURL, zoomJWT)
			if err != nil {
				err = fmt.Errorf("failed to request download file: %w", err)
				log.Println(err)
				return
			}

			defer body.Close()

			fmt.Println("Getting writer", fileName)
			sw := storageWriter(ctx, storageClient, bucket, gstoragePath+"/"+recording.FileName())
			fmt.Println("Copying", fileName)
			if _, err := io.Copy(sw, body); err != nil {
				err = fmt.Errorf("Could not write file: %v", err)
				log.Println(err)
			}

			fmt.Println("Closing", fileName)
			if err := sw.Close(); err != nil {
				err = fmt.Errorf("Could not put file: %v", err)
				log.Println(err)
			}
			fmt.Println("Finished", recording.FileName())
			wg.Done()
		}(recording)

		fmt.Println("Started", recording.FileName())
	}

	wg.Wait()
}

func (f recordingFile) FileName() string {
	return fmt.Sprintf(
		"%s-%ss.%s",
		f.RecordingStart,
		f.RecordingType,
		strings.ToLower(f.FileType),
	)
}

func requestRecordingFile(fileURL, zoomJWT string) (io.ReadCloser, error) {
	recURL := fileURL + "?access_token=" + zoomJWT
	req, err := http.NewRequest("GET", recURL, nil)
	if err != nil {
		err = fmt.Errorf("failed to create new HTTP request for recording download: %w", err)
		return nil, err
	}
	req.Header.Add("Accept", "application/json")
	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		err = fmt.Errorf("failed to perform request to download recording: %w", err)
		return nil, err
	}

	if resp.StatusCode/200 != 1 {
		err = fmt.Errorf("invalid recording download response code: %d", resp.StatusCode)
		return nil, err
	}

	return resp.Body, nil
}

func fetchRecordings(zoomJWT, zoomUserID string) ([]recordingFile, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf(zoomRecodingsURL, zoomUserID), nil)
	if err != nil {
		err = fmt.Errorf("failed to create new HTTP request for recordings: %w", err)
		return nil, err
	}
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Authorization", "Bearer "+zoomJWT)
	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		err = fmt.Errorf("failed to perform request for recordings: %w", err)
		return nil, err
	}

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		err = fmt.Errorf("failed to read recordings response body: %w", err)
		return nil, err
	}

	if resp.StatusCode/200 != 1 {
		err = fmt.Errorf("invalid recordings response code: %s", buf.String())
		return nil, err
	}

	response := &recordingListResponse{}
	err = json.Unmarshal(buf.Bytes(), response)
	if err != nil {
		err = fmt.Errorf("failed to unmarshal recordings response: %w", err)
		return nil, err
	}

	recordings := []recordingFile{}
	for _, meeting := range response.Meetings {
		for _, file := range meeting.RecordingFiles {
			if file.Status == "completed" && file.FileType == "MP4" {
				recordings = append(recordings, file)
			}

		}
	}

	return recordings, nil
}

var defaultHTTPClient = &http.Client{
	Timeout: time.Second * 15 * 60,
	Transport: &http.Transport{
		Dial: (&net.Dialer{
			Timeout: time.Second * 10,
		}).Dial,
		TLSHandshakeTimeout: time.Second * 10,
	},
}

func storageWriter(ctx context.Context, storageClient *storage.Client, bucket, filename string) *storage.Writer {
	return storageClient.Bucket(bucket).Object(filename).NewWriter(ctx)
}
