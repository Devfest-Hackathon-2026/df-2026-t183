package synthesis

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/httptransport"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type testTokenProvider struct{}

func (testTokenProvider) Token(context.Context) (*auth.Token, error) {
	return &auth.Token{Value: "not-a-real-token"}, nil
}

func TestQuotaProjectHeadersFollowCredentials(t *testing.T) {
	for _, project := range []string{"test-quota-project", ""} {
		t.Run("quota="+project, func(t *testing.T) {
			t.Setenv("GOOGLE_CLOUD_QUOTA_PROJECT", project)
			opts := transportOptions(auth.NewCredentials(&auth.CredentialsOptions{TokenProvider: testTokenProvider{}}))
			if opts.Headers == nil {
				t.Fatal("auth transport requires an initialized header map")
			}
			calls := 0
			opts.BaseRoundTripper = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if got := r.Header.Get("X-Goog-User-Project"); got != project {
					t.Errorf("quota project = %q, want %q", got, project)
				}
				return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
			})
			httpClient, err := httptransport.NewClient(opts)
			if err != nil {
				t.Fatal(err)
			}
			_, err = (&Client{httpClient: httpClient}).Synthesize(context.Background(), "質問？")
			if !errors.Is(err, ErrUnavailable) || calls != 1 {
				t.Fatal("unexpected transport result", err, calls)
			}
		})
	}
}

func testClient(status int, body string, inspect func(*http.Request)) *Client {
	return &Client{httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if inspect != nil {
			inspect(r)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
}

func TestClientSendsZeroJapaneseVoiceMP3Request(t *testing.T) {
	client := testClient(http.StatusServiceUnavailable, `{}`, func(r *http.Request) {
		if r.Method != http.MethodPost || r.URL.String() != endpoint || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request: %s %s %v", r.Method, r.URL, r.Header)
		}
		var body struct {
			Input struct {
				Text string `json:"text"`
			} `json:"input"`
			Voice       struct{ LanguageCode, Name string } `json:"voice"`
			AudioConfig struct{ AudioEncoding string }      `json:"audioConfig"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.Input.Text != "配列とは？" || body.Voice.LanguageCode != "ja-JP" || body.Voice.Name != "ja-JP-Chirp3-HD-Puck" || body.AudioConfig.AudioEncoding != "MP3" {
			t.Errorf("unexpected request body: %+v", body)
		}
	})
	got, err := client.Synthesize(context.Background(), "配列とは？")
	if !errors.Is(err, ErrUnavailable) || got != nil {
		t.Fatalf("unexpected response: bytes=%d err=%v", len(got), err)
	}
}

func TestClientMapsRemoteFailures(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   error
	}{
		{401, ErrInternal}, {403, ErrInternal}, {404, ErrInternal},
		{408, context.DeadlineExceeded}, {429, ErrUnavailable}, {500, ErrUnavailable}, {504, context.DeadlineExceeded},
		{400, ErrFailed},
	} {
		client := testClient(tc.status, `{"error":{"message":"secret"}}`, nil)
		_, err := client.Synthesize(context.Background(), "質問？")
		if !errors.Is(err, tc.want) {
			t.Errorf("status %d: got %v, want %v", tc.status, err, tc.want)
		}
	}
}

func TestClientRejectsMalformedAudio(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`{"audioContent":null}`,
		`{"audioContent":""}`,
		`{"audioContent":"%%%"}`,
		`{"audioContent":"` + base64.StdEncoding.EncodeToString([]byte("not an mp3")) + `"}`,
		`{"audioContent":"abc","audioContent":"def"}`,
		`{"audioContent":"\ud800"}`,
	} {
		client := testClient(http.StatusOK, body, nil)
		_, err := client.Synthesize(context.Background(), "質問？")
		if !errors.Is(err, ErrFailed) {
			t.Errorf("body %q: got %v, want ErrFailed", body, err)
		}
	}
}

func TestMP3FrameChecksWithoutFFmpeg(t *testing.T) {
	audio, err := os.ReadFile("testdata/silence.mp3")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	for _, tc := range []struct {
		name string
		data []byte
		want bool
	}{
		{"valid fixed MP3", audio, true},
		{"empty", nil, false},
		{"text", []byte("not an mp3"), false},
		{"ID3 header only", audio[:10], false},
		{"truncated frame", audio[:len(audio)-1], false},
		{"frame header only", []byte{0xff, 0xfb, 0x90, 0x00}, false},
		{"invalid ID3 size", []byte{'I', 'D', '3', 4, 0, 0, 0x80, 0, 0, 0}, false},
		{"trailing garbage", append(append([]byte{}, audio...), 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasMP3Frames(tc.data); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			if !tc.want {
				data, _ := json.Marshal(map[string]string{"audioContent": base64.StdEncoding.EncodeToString(tc.data)})
				_, err := testClient(http.StatusOK, string(data), nil).Synthesize(context.Background(), "質問？")
				if !errors.Is(err, ErrFailed) {
					t.Fatalf("invalid MP3 response: %v", err)
				}
			}
		})
	}
}

func TestClientDecodesFixedMP3WithFFmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("real MP3 decoder unavailable; frame checks run separately")
	}
	audio, err := os.ReadFile("testdata/silence.mp3")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]string{"audioContent": base64.StdEncoding.EncodeToString(audio)})
	got, err := testClient(http.StatusOK, string(data), nil).Synthesize(context.Background(), "配列とは？")
	if err != nil || !bytes.Equal(got, audio) {
		t.Fatalf("audio mismatch: bytes=%d err=%v", len(got), err)
	}
}

func TestMP3SupportedFrameLayoutsWithoutFFmpeg(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	frame := func(header byte, version byte, size int) []byte {
		data := make([]byte, size)
		copy(data, []byte{0xff, version, header, 0})
		return data
	}
	mpeg1 := frame(0x90, 0xfb, 417)
	mpeg2 := frame(0x80, 0xf3, 208)
	mpeg25 := frame(0x80, 0xe3, 417)
	id3 := []byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 1, 0}
	tag := make([]byte, 128)
	copy(tag, "TAG")
	for _, tc := range []struct {
		name  string
		audio []byte
	}{
		{"MPEG 1", mpeg1}, {"MPEG 2", mpeg2}, {"MPEG 2.5", mpeg25},
		{"variable bitrate", append(append([]byte{}, mpeg1...), frame(0xa0, 0xfb, 522)...)},
		{"ID3v2 prefix", append(id3, mpeg1...)},
		{"ID3v1 suffix", append(append([]byte{}, mpeg1...), tag...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !hasMP3Frames(tc.audio) {
				t.Fatal("supported MP3 frame layout rejected")
			}
		})
	}
}

func TestClientWithoutMP3DecoderFailsClosed(t *testing.T) {
	audio, err := os.ReadFile("testdata/silence.mp3")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	data, _ := json.Marshal(map[string]string{"audioContent": base64.StdEncoding.EncodeToString(audio)})
	got, err := testClient(http.StatusOK, string(data), nil).Synthesize(context.Background(), "質問？")
	if got != nil || !errors.Is(err, ErrInternal) {
		t.Fatalf("decoder missing but audio accepted: %v", err)
	}
}
