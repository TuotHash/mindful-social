package server

import (
	"bytes"
	"encoding/base64"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func TestAccountProfileImageUpload(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	user := signupAndGetUser(t, c, "alice", "alice@example.com", "correct horse battery staple")

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("gorilla.csrf.Token", fetchCSRFToken(t, c)); err != nil {
		t.Fatalf("write csrf field: %v", err)
	}
	fw, err := mw.CreateFormFile(profileImageFormField, "avatar.png")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	if _, err := fw.Write(png); err != nil {
		t.Fatalf("write png: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, testTS.URL+"/account/profile-image", &body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("upload profile image: %v", err)
	}
	page := readBody(t, resp)
	if !strings.Contains(page, "Profile picture updated.") {
		t.Fatalf("account page missing success message; body: %s", snippet(page))
	}

	updated, err := testServer.queries.GetUser(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("get updated user: %v", err)
	}
	if updated.ProfileImagePath == "" {
		t.Fatal("profile image path was not stored")
	}
	imageResp := get(t, c, updated.ProfileImagePath)
	defer imageResp.Body.Close()
	if imageResp.StatusCode != http.StatusOK {
		t.Fatalf("uploaded image status = %d, want 200", imageResp.StatusCode)
	}
}
