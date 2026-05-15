package server

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/TuotHash/mindful-social/internal/db"
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

func TestAccountProfileImageUpload_CompressesAndResizes(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	_ = signupAndGetUser(t, c, "charlie", "charlie@example.com", "correct horse battery staple")

	var source bytes.Buffer
	src := image.NewRGBA(image.Rect(0, 0, 4200, 3200))
	for y := 0; y < 3200; y++ {
		for x := 0; x < 4200; x++ {
			src.SetRGBA(x, y, color.RGBA{
				R: uint8((x*17 + y*11) % 255),
				G: uint8((x*29 + y*7) % 255),
				B: uint8((x*3 + y*23) % 255),
				A: 255,
			})
		}
	}
	if err := png.Encode(&source, src); err != nil {
		t.Fatalf("encode source png: %v", err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("gorilla.csrf.Token", fetchCSRFToken(t, c)); err != nil {
		t.Fatalf("write csrf field: %v", err)
	}
	fw, err := mw.CreateFormFile(profileImageFormField, "big.png")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(source.Bytes()); err != nil {
		t.Fatalf("write source png: %v", err)
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

	profileBody := readBody(t, get(t, c, "/users/charlie"))
	if !strings.Contains(profileBody, "/uploads/profiles/") {
		t.Fatalf("profile should reference uploaded image; body: %s", snippet(profileBody))
	}

	// Reload account to read the persisted path and inspect the served file.
	accountBody := readBody(t, get(t, c, "/account"))
	idx := strings.Index(accountBody, "/uploads/profiles/")
	if idx < 0 {
		t.Fatalf("account should reference uploaded image path; body: %s", snippet(accountBody))
	}
	rest := accountBody[idx:]
	end := strings.Index(rest, "\"")
	if end < 0 {
		t.Fatalf("malformed uploaded image path in account html")
	}
	publicPath := rest[:end]

	imageResp := get(t, c, publicPath)
	raw := readBody(t, imageResp)
	if imageResp.StatusCode != http.StatusOK {
		t.Fatalf("uploaded image status = %d, want 200", imageResp.StatusCode)
	}
	if len(raw) > maxCompressedImageBytes {
		t.Fatalf("compressed image is %d bytes, want <= %d", len(raw), maxCompressedImageBytes)
	}
	outImg, _, err := image.Decode(bytes.NewReader([]byte(raw)))
	if err != nil {
		t.Fatalf("decode compressed image: %v", err)
	}
	b := outImg.Bounds()
	if b.Dx() > maxProfileImageDimension || b.Dy() > maxProfileImageDimension {
		t.Fatalf("compressed image dimensions = %dx%d, want max %d", b.Dx(), b.Dy(), maxProfileImageDimension)
	}
}

func TestAccountListsOwnNodeMediaUploads(t *testing.T) {
	integrationDB(t)
	aliceClient := newClient(t)
	alice := signupAndGetUser(t, aliceClient, "alice", "alice@example.com", "correct horse battery staple")
	topicID := createNode(t, aliceClient, "topic", "Media Topic", "")
	topic, err := testServer.queries.GetNode(t.Context(), topicID)
	if err != nil {
		t.Fatalf("get topic: %v", err)
	}

	if _, err := testServer.queries.CreateNodeImage(t.Context(), db.CreateNodeImageParams{
		RootTopicID: topic.ID,
		UploadedBy:  alice.ID,
		StoredPath:  "/uploads/topics/" + topic.ID.String() + "/alice-image.jpg",
		ContentType: "image/jpeg",
		ByteSize:    2048,
	}); err != nil {
		t.Fatalf("create image media: %v", err)
	}
	if _, err := testServer.queries.CreateNodeVideo(t.Context(), db.CreateNodeVideoParams{
		RootTopicID: topic.ID,
		UploadedBy:  alice.ID,
		StoredPath:  "/uploads/topics/" + topic.ID.String() + "/alice-video.mp4",
		ContentType: "video/mp4",
		ByteSize:    4096,
		Width:       640,
		Height:      360,
		DurationMs:  61000,
	}); err != nil {
		t.Fatalf("create video media: %v", err)
	}

	bobClient := newClient(t)
	bob := signupAndGetUser(t, bobClient, "bob", "bob@example.com", "correct horse battery staple")
	if _, err := testServer.queries.CreateNodeImage(t.Context(), db.CreateNodeImageParams{
		RootTopicID: topic.ID,
		UploadedBy:  bob.ID,
		StoredPath:  "/uploads/topics/" + topic.ID.String() + "/bob-image.jpg",
		ContentType: "image/jpeg",
		ByteSize:    1024,
	}); err != nil {
		t.Fatalf("create bob image media: %v", err)
	}

	body := readBody(t, get(t, aliceClient, "/account"))
	for _, want := range []string{"Media uploads", "alice-image.jpg", "alice-video.mp4", "Media Topic", "640x360", "1:01"} {
		if !strings.Contains(body, want) {
			t.Fatalf("account media list missing %q; body: %s", want, snippet(body))
		}
	}
	if strings.Contains(body, "bob-image.jpg") {
		t.Fatalf("account media list included another user's upload; body: %s", snippet(body))
	}
}
