package zoombackup

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
	"time"

	"cloud.google.com/go/storage"
	"github.com/dgrijalva/jwt-go"
	"github.com/gobuffalo/envy"
	"google.golang.org/api/iterator"
)

const (
	zoomRecordingsURL       = "https://api.zoom.us/v2/users/%s/recordings?from=%s"
	zoomDeleteRecordingsURL = "https://api.zoom.us/v2/meetings/%s/recordings"
	ymdFormat               = "2006-01-02"
	tokenExpiresIn          = 35 * time.Minute
	dateFormatFrom          = "2006-01-02"
	dateFormatTo            = "01-02-2006" // MM-DD-YYYY
	dateLength              = 10
)

type recordingListResponse struct {
	Meetings []struct {
		ID             string          `json:"uuid"`
		Topic          string          `json:"topic"`
		StartTime      string          `json:"start_time"`
		RecordingFiles []recordingFile `json:"recording_files"`
	} `json:"meetings"`
}

type meeting struct {
	ID        string          `json:"id"`
	Topic     string          `json:"topic"`
	StartTime string          `json:"start_time"`
	Files     []recordingFile `json:"files"`
}

type recordingFile struct {
	RecordingStart string `json:"recording_start"`
	FileType       string `json:"file_type"`
	DownloadURL    string `json:"download_url"`
	RecordingType  string `json:"recording_type"`
	Status         string `json:"status"`
}

func ZoomBackup(w http.ResponseWriter, r *http.Request) {
	zoomAPIKey := envy.Get("ZOOM_API_KEY", "")
	if zoomAPIKey == "" {
		log.Fatal("Please set ZOOM_API_KEY to access the zoom API.")
	}

	zoomAPISecret := envy.Get("ZOOM_API_SECRET", "")
	if zoomAPISecret == "" {
		log.Fatal("Please set ZOOM_API_SECRET to access the zoom API.")
	}

	zoomJWT, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &jwt.StandardClaims{
		ExpiresAt: time.Now().Add(tokenExpiresIn).Unix(),
		Issuer:    zoomAPIKey,
	}).SignedString([]byte(zoomAPISecret))
	if err != nil {
		err = fmt.Errorf("failed to sign JWT: %w", err)
		log.Fatal(err)
	}

	zoomUserID := envy.Get("ZOOM_USER_ID", "")
	if zoomUserID == "" {
		log.Fatal("Please set ZOOM_USER_ID from which to retreive recording.")
	}

	bucket := envy.Get("GSTORAGE_BUCKET", "")
	if bucket == "" {
		log.Fatal("Please set GSTORAGE_BUCKET with a bucket as a backup destination")
	}

	ctx := context.Background()

	storageClient, err := storage.NewClient(ctx)
	if err != nil {
		log.Fatal("Error creating new storage client ", err)
	}

	meetings, err := fetchRecordings(zoomJWT, zoomUserID)
	if err != nil {
		err = fmt.Errorf("failed to fetch recordings: %w", err)
		log.Fatal(err)
	}

	for _, meeting := range meetings {
		for _, recording := range meeting.Files {
			fileName := recording.FileName()
			log.Println("Requesting", fileName)
			body, err := requestRecordingFile(recording.DownloadURL, zoomJWT)
			if err != nil {
				err = fmt.Errorf("failed to request download file: %w", err)
				log.Println(err)
				continue
			}

			defer body.Close()

			fileSaveName, err := getFileSaveName(meeting.StartTime, meeting.Topic, recording.FileName())
			if err != nil {
				err = fmt.Errorf("failed to get file save name: %w", err)
				log.Println(err)
				continue
			}
			log.Println("Getting writer", fileSaveName)

			sw := storageWriter(ctx, storageClient, bucket, fileSaveName)
			log.Println("Copying", fileName)
			if _, err := io.Copy(sw, body); err != nil {
				err = fmt.Errorf("Could not write file: %v", err)
				log.Println(err)
				continue
			}

			log.Println("Closing", fileName)
			if err := sw.Close(); err != nil {
				err = fmt.Errorf("Could not put file: %v", err)
				log.Println(err)
				continue
			}
			log.Println("Finished", recording.FileName())
		}

		log.Println("Deleting recordings for", meeting.ID)
		err = deleteMeetingRecordings(zoomJWT, meeting.ID)
		if err != nil {
			log.Println(err)
		}
	}
	if err := generateURLSListHTML(ctx, storageClient, bucket); err != nil {
		err = fmt.Errorf("Could not generate html file: %v", err)
		log.Println(err)
	}
}

func (f recordingFile) FileName() string {
	return fmt.Sprintf(
		"%s-%s.%s",
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

func fetchRecordings(zoomJWT, zoomUserID string) ([]meeting, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf(zoomRecordingsURL, zoomUserID, time.Now().AddDate(0, -1, 0).Format(ymdFormat)), nil)
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

	meetings := make([]meeting, len(response.Meetings))
	for i, meeting := range response.Meetings {
		meetings[i].ID = meeting.ID
		meetings[i].Topic = meeting.Topic
		meetings[i].StartTime = meeting.StartTime
		for _, file := range meeting.RecordingFiles {
			if file.Status == "completed" && file.FileType == "MP4" {
				meetings[i].Files = append(meetings[i].Files, file)
			}

		}
	}

	return meetings, nil
}

func deleteMeetingRecordings(zoomJWT, meetingID string) error {
 	req, err := http.NewRequest("DELETE", fmt.Sprintf(zoomDeleteRecordingsURL, meetingID), nil)
 	if err != nil {
 		err = fmt.Errorf("failed to create new HTTP request to delete recordings: %w", err)
 		return err
 	}
 	req.Header.Add("Accept", "application/json")
 	req.Header.Add("Authorization", "Bearer "+zoomJWT)
 	resp, err := defaultHTTPClient.Do(req)
 	if err != nil {
 		err = fmt.Errorf("failed to delete recordings: %w", err)
 		return err
 	}

 	if resp.StatusCode != http.StatusNoContent {
 		buf := new(bytes.Buffer)
 		_, err = buf.ReadFrom(resp.Body)
 		_ = resp.Body.Close()
 		if err != nil {
 			err = fmt.Errorf("failed to read delete recordings response body: %w", err)
 			return err
 		}
 		err = fmt.Errorf("invalid delete recordings response code: %d -- %s", resp.StatusCode, buf.String())
 		return err
 	}

 	return nil
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

func getFileSaveName(startTime, Topic, recordingFileName string) (string, error) {
	var folderName string
	meetingDate, err := time.Parse(dateFormatFrom, startTime[:dateLength])
	if err != nil {
		return "", fmt.Errorf("failed to parse date: %w", err)
	}

	if Topic != "" {
		folderName = fmt.Sprintf("%s-", Topic)
	}
	folderName += fmt.Sprintf("%s", meetingDate.Format(dateFormatTo))
	fileSaveName := folderName + "/" + recordingFileName
	return fileSaveName, nil
}

func generateURLSListHTML(ctx context.Context, storageClient *storage.Client, bucket string) error {
	html := openHTML()
	it := storageClient.Bucket(bucket).Objects(ctx, nil)
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("Bucket(%q).Objects: %v", bucket, err)
		}
		html += addLinkHTML(bucket, attrs.Name)
	}

	html += closeHTML()

	htmlFileName := "biga.html"

	obj := storageClient.Bucket(bucket).Object(htmlFileName)
	wc := obj.NewWriter(ctx)
	if _, err := io.Copy(wc, bytes.NewBuffer([]byte(html))); err != nil {
		return fmt.Errorf("Could not write file: %v", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("error closing: %w", err)
	}
	return nil
}

func openHTML() string {
	return "<html><body><h2>Kitchen Rodeo Meetings</h2><ul>"
}

func closeHTML() string {
	return "</ul></body></html>"
}

func addLinkHTML(bucket, fileName string) string {
	link := fmt.Sprintf("http://%s/%s", bucket, fileName)
	return fmt.Sprintf("<li><a href=\"%s\">%s</a></li>", link, fileName)
}
