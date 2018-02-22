package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type googleS3 struct {
	bucket string // no slashes
	path   string // starts and ends with slash
}

var _ destination = &googleS3{}

// Make HTTP authorization header for AWS-style authentication.
func (r *googleS3) authorize(msg string) string {
	h := hmac.New(sha1.New, []byte(config.GoogleS3.Secret))
	h.Write([]byte(msg))
	sig := base64.StdEncoding.EncodeToString(h.Sum(nil))
	return fmt.Sprintf("AWS %s:%s", config.GoogleS3.AccessKey, sig)
}

// List returns filenames, ordered by name.
func (r *googleS3) List() (names []string, err error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", "https://storage.googleapis.com/"+r.bucket+"/?prefix="+url.QueryEscape(r.path[1:]), nil)
	if err != nil {
		return nil, err
	}

	date := time.Now().UTC().Format(time.RFC1123Z)
	req.Header.Add("Date", date)

	msg := "GET\n"
	msg += "\n"
	msg += "\n"
	msg += date + "\n"
	msg += "/" + r.bucket + "/"

	req.Header.Add("Authorization", r.authorize(msg))

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("listing %s: status code not 200 but %d", "/"+r.bucket+r.path, resp.StatusCode)
	}

	var list struct {
		Key []string `xml:"Contents>Key"`
	}
	err = xml.NewDecoder(resp.Body).Decode(&list)
	if err != nil {
		return nil, fmt.Errorf("parsing directory contents xml: %s", err)
	}
	err = resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("closing http reponse: %s", err)
	}
	prefix := len(r.path[1:])
	for i, name := range list.Key {
		list.Key[i] = name[prefix:]
	}
	// the list is returned sorted by google cloud storage
	return list.Key, nil
}

func (r *googleS3) Open(path string) (rc io.ReadCloser, err error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", "https://storage.googleapis.com/"+r.bucket+url.PathEscape(r.path+path), nil)
	if err != nil {
		return nil, err
	}

	date := time.Now().UTC().Format(time.RFC1123Z)
	req.Header.Add("Date", date)

	msg := "GET\n"
	msg += "\n"
	msg += "\n"
	msg += date + "\n"
	msg += "/" + r.bucket + url.PathEscape(r.path+path)

	req.Header.Add("Authorization", r.authorize(msg))

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("opening %s: status code not 200 but %d", path, resp.StatusCode)
	}
	return resp.Body, nil
}

type s3writer struct {
	p   *io.PipeWriter
	err chan error // for waiting until request has completed
}

func (x *s3writer) Write(buf []byte) (int, error) {
	return x.p.Write(buf)
}

func (x *s3writer) Close() error {
	err := x.p.Close()
	if err != nil {
		return err
	}
	return <-x.err
}

func (r *googleS3) Create(path string) (w io.WriteCloser, err error) {
	client := &http.Client{}
	req, err := http.NewRequest("PUT", "https://storage.googleapis.com/"+r.bucket+url.PathEscape(r.path+path), nil)
	if err != nil {
		return nil, err
	}

	date := time.Now().UTC().Format(time.RFC1123Z)
	req.Header.Add("Date", date)

	msg := "PUT\n"
	msg += "\n"
	msg += "\n"
	msg += date + "\n"
	msg += "/" + r.bucket + url.PathEscape(r.path+path)

	req.Header.Add("Authorization", r.authorize(msg))

	req.ContentLength = 0
	pr, pw := io.Pipe()
	req.Body = pr

	s3w := &s3writer{p: pw}
	s3w.err = make(chan error, 1)
	go func() {
		resp, err := client.Do(req)
		if err != nil {
			pr.CloseWithError(err)
			s3w.err <- err
			return
		}
		if resp.StatusCode != 200 {
			pr.CloseWithError(fmt.Errorf("creating %s: status code not 200 but %d", path, resp.StatusCode))
			s3w.err <- err
			return
		}
		s3w.err <- nil
	}()
	return s3w, nil
}

func (r *googleS3) Rename(opath, npath string) (err error) {
	client := &http.Client{}
	req, err := http.NewRequest("PUT", "https://storage.googleapis.com/"+r.bucket+url.PathEscape(r.path+npath), nil)
	if err != nil {
		return fmt.Errorf("creating http request: %s", err)
	}

	date := time.Now().UTC().Format(time.RFC1123Z)
	req.Header.Add("Date", date)
	copySource := "/" + r.bucket + url.PathEscape(r.path+opath)
	req.Header.Add("x-amz-copy-source", copySource)

	msg := "PUT\n"
	msg += "\n"
	msg += "\n"
	msg += date + "\n"
	msg += fmt.Sprintf("x-amz-copy-source:%s\n", copySource)
	msg += "/" + r.bucket + url.PathEscape(r.path+npath)

	req.Header.Add("Authorization", r.authorize(msg))
	req.ContentLength = 0

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http request for copying resource: %s", err)
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("copying resource, http status not 200 but %d", resp.StatusCode)
	}

	err = r.Delete(opath)
	if err != nil {
		return fmt.Errorf("deleting original resource after copying: %s", err)
	}
	return nil
}

func (r *googleS3) Delete(path string) (err error) {
	client := &http.Client{}
	req, err := http.NewRequest("DELETE", "https://storage.googleapis.com/"+r.bucket+url.PathEscape(r.path+path), nil)
	if err != nil {
		return err
	}

	date := time.Now().UTC().Format(time.RFC1123Z)
	req.Header.Add("Date", date)

	msg := "DELETE\n"
	msg += "\n"
	msg += "\n"
	msg += date + "\n"
	msg += "/" + r.bucket + url.PathEscape(r.path+path)

	req.Header.Add("Authorization", r.authorize(msg))

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	err = resp.Body.Close()
	if resp.StatusCode != 204 {
		return fmt.Errorf("deleting %s: status code not 204 but %d", path, resp.StatusCode)
	}
	return err
}
