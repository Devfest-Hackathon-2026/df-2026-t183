package speech

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testClient(status int, body string) *Client {
	return &Client{endpoint: "https://speech.example.test/recognize", httpClient: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
}

func TestTranscribeResponseShape(t *testing.T) {
	cases := []struct {
		name, body, text string
		invalid          bool
	}{
		{"omitted results", `{}`, "", false},
		{"empty results", `{"results":[]}`, "", false},
		{"whitespace", `{"results":[{"alternatives":[{"transcript":"　 \n\t"}]}]}`, "", false},
		{"segments", `{"results":[{"alternatives":[{"transcript":"　配列は"},{"transcript":"ignored"}]},{"alternatives":[{"transcript":"順番です。　"}]}]}`, "配列は順番です。", false},
		{"empty body", ``, "", true}, {"null root", `null`, "", true}, {"array root", `[]`, "", true},
		{"scalar root", `"text"`, "", true}, {"invalid JSON", `{`, "", true},
		{"null results", `{"results":null}`, "", true}, {"object results", `{"results":{}}`, "", true},
		{"string results", `{"results":""}`, "", true}, {"null item", `{"results":[null]}`, "", true},
		{"missing alternatives", `{"results":[{}]}`, "", true},
		{"null alternatives", `{"results":[{"alternatives":null}]}`, "", true},
		{"empty alternatives", `{"results":[{"alternatives":[]}]}`, "", true},
		{"null best", `{"results":[{"alternatives":[null]}]}`, "", true},
		{"missing transcript", `{"results":[{"alternatives":[{}]}]}`, "", true},
		{"null transcript", `{"results":[{"alternatives":[{"transcript":null}]}]}`, "", true},
		{"wrong transcript", `{"results":[{"alternatives":[{"transcript":42}]}]}`, "", true},
		{"duplicate results", `{"results":[],"results":[]}`, "", true},
		{"unpaired surrogate", `{"results":[{"alternatives":[{"transcript":"\ud800"}]}]}`, "", true},
		{"invalid UTF8", "{\"results\":[{\"alternatives\":[{\"transcript\":\"\xff\"}]}]}", "", true},
		{"too large response", strings.Repeat(" ", (1<<20)+1), "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := testClient(200, tc.body).Transcribe(context.Background(), []byte("synthetic"))
			if tc.invalid {
				if !errors.Is(err, ErrFailed) {
					t.Fatalf("got %q, %v; want ErrFailed", got, err)
				}
			} else if err != nil || got != tc.text {
				t.Fatalf("got %q, %v; want %q", got, err, tc.text)
			}
		})
	}
}

func TestTranscribeHTTPFailures(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   error
	}{
		{400, ErrFailed}, {401, ErrInternal}, {403, ErrInternal}, {404, ErrInternal}, {408, context.DeadlineExceeded},
		{429, ErrUnavailable}, {500, ErrUnavailable}, {503, ErrUnavailable}, {504, context.DeadlineExceeded}, {302, ErrFailed},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			_, err := testClient(tc.status, "private upstream details").Transcribe(context.Background(), []byte("synthetic"))
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestTranscribeCancellation(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if deadline {
			ctx, cancel = context.WithTimeout(context.Background(), 0)
		}
		cancel()
		c := testClient(200, `{}`)
		c.httpClient.Transport = transportFunc(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })
		_, err := c.Transcribe(ctx, []byte("synthetic"))
		if !errors.Is(err, ctx.Err()) {
			t.Fatalf("got %v; want %v", err, ctx.Err())
		}
	}
}
