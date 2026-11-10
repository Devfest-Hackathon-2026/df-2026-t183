package speech

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

func audioValidator(t *testing.T) *AudioValidator {
	t.Helper()
	v, err := NewValidator()
	if err != nil {
		if os.Getenv("REQUIRE_AUDIO_TESTS") == "1" {
			t.Fatal("Linux ffmpeg / ffprobe is required")
		}
		t.Skip("Linux ffmpeg / ffprobe is unavailable")
	}
	return v
}

func generatedAudio(t *testing.T, format, codec, duration string) []byte {
	t.Helper()
	args := []string{"-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", duration, "-c:a", codec}
	if format == "mp4" {
		args = append(args, "-movflags", "frag_keyframe+empty_moov")
	}
	args = append(args, "-f", format, "pipe:1")
	data, err := exec.Command("ffmpeg", args...).Output()
	if err != nil {
		t.Fatalf("generate audio: %v", err)
	}
	return data
}

func TestAudioValidatorBoundaries(t *testing.T) {
	v := audioValidator(t)
	for _, format := range []struct{ format, codec, mime string }{{"webm", "libopus", "audio/webm;codecs=opus"}, {"ogg", "libopus", "audio/ogg;codecs=opus"}, {"mp4", "aac", "audio/mp4;codecs=mp4a.40.2"}} {
		for _, duration := range []struct {
			duration string
			tooLarge bool
		}{{"1", false}, {"30", false}, {"30.5", false}, {"31", false}, {"31.1", true}, {"32", true}} {
			t.Run(format.format+"/"+duration.duration, func(t *testing.T) {
				data := generatedAudio(t, format.format, format.codec, duration.duration)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				err := v.Validate(ctx, data, format.mime)
				if duration.tooLarge || (format.format == "mp4" && duration.duration == "31") {
					// AAC frames add padding: the generated 31s fragmented MP4 decodes beyond 31s.
					if !errors.Is(err, ErrTooLarge) {
						t.Fatalf("got %v want ErrTooLarge", err)
					}
				} else if err != nil {
					t.Fatalf("valid audio rejected: %v", err)
				}
			})
		}
	}
}

func TestAudioValidatorInvalid(t *testing.T) {
	v := audioValidator(t)
	webm := generatedAudio(t, "webm", "libopus", "1")
	for _, tc := range []struct {
		name string
		data []byte
		mime string
		want error
	}{
		{"empty", nil, "audio/webm", ErrInvalid}, {"random", []byte("invalid"), "audio/webm", ErrInvalid},
		{"wrong container", webm, "audio/mp4", ErrInvalid}, {"wrong codec", generatedAudio(t, "webm", "libvorbis", "1"), "audio/webm", ErrInvalid},
		{"header only", webm[:40], "audio/webm", ErrInvalid}, {"unsupported", webm, "video/webm", ErrUnsupported},
		{"bad media type", webm, "audio/webm;codecs", ErrUnsupported},
		{"matroska disguised as webm", generatedAudio(t, "matroska", "libopus", "1"), "audio/webm", ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := v.Validate(ctx, tc.data, tc.mime)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := v.Validate(ctx, webm, "audio/webm"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestWebMDocType(t *testing.T) {
	header := []byte{0x1a, 0x45, 0xdf, 0xa3, 0x87, 0x42, 0x82, 0x84, 'w', 'e', 'b', 'm'}
	if !hasWebMDocType(header) {
		t.Fatal("valid header rejected")
	}
	if hasWebMDocType(append([]byte{0x1a, 0x45, 0xdf, 0xa3, 0x88, 0x42, 0x82, 0x85}, []byte("otherwebm")...)) {
		t.Fatal("substring accepted")
	}
	if hasWebMDocType(bytes.Replace(header, []byte("webm"), []byte("WEBM"), 1)) {
		t.Fatal("wrong DocType accepted")
	}
}

func TestAudioTimestampGaps(t *testing.T) {
	v := audioValidator(t)
	data, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "32", "-af", "aselect=lt(t\\,0.1)+gt(t\\,31.9)", "-c:a", "libopus", "-f", "webm", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := v.Validate(ctx, data, "audio/webm"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("timestamp gap bypassed duration limit: %v", err)
	}
}

func TestAudioRejectsCompressedTimestamps(t *testing.T) {
	v := audioValidator(t)
	original := generatedAudio(t, "webm", "libopus", "40")
	cmd := exec.Command("ffmpeg", "-v", "error", "-itsscale", "0.5", "-i", "pipe:0", "-c", "copy", "-f", "webm", "pipe:1")
	cmd.Stdin = bytes.NewReader(original)
	scaled, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := v.Validate(ctx, scaled, "audio/webm"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("40 seconds of samples with 20-second timestamps: got %v, want ErrTooLarge", err)
	}
}

func TestAudioRejectsTruncatedWebM(t *testing.T) {
	v := audioValidator(t)
	original := generatedAudio(t, "webm", "libopus", "40")
	if len(original) < 6000 {
		t.Fatal("fixture too short")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := v.Validate(ctx, original[:6000], "audio/webm"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("truncated audio accepted despite decoder warning: %v", err)
	}
}

func TestAudioRejectsDigitalSilence(t *testing.T) {
	v := audioValidator(t)
	for _, format := range []struct{ format, codec, mime string }{{"webm", "libopus", "audio/webm"}, {"ogg", "libopus", "audio/ogg"}, {"mp4", "aac", "audio/mp4"}} {
		t.Run(format.format, func(t *testing.T) {
			args := []string{"-v", "error", "-f", "lavfi", "-i", "anullsrc=r=48000:cl=mono", "-t", "1", "-c:a", format.codec}
			if format.format == "mp4" {
				args = append(args, "-movflags", "frag_keyframe+empty_moov")
			}
			args = append(args, "-f", format.format, "pipe:1")
			data, err := exec.Command("ffmpeg", args...).Output()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := v.Validate(ctx, data, format.mime); !errors.Is(err, ErrInvalid) {
				t.Fatalf("digital silence must be rejected before recognition: got %v", err)
			}
		})
	}
}

func TestAudioAllowsQuietSignal(t *testing.T) {
	v := audioValidator(t)
	for _, format := range []struct{ format, codec, mime string }{{"webm", "libopus", "audio/webm"}, {"ogg", "libopus", "audio/ogg"}, {"mp4", "aac", "audio/mp4"}} {
		for _, source := range []struct{ name, input string }{
			{"below 16-bit precision", "aevalsrc=0.00001*sin(2*PI*440*t):s=48000"},
			{"opposite stereo phase", "aevalsrc=0.1*sin(2*PI*440*t)|-0.1*sin(2*PI*440*t):s=48000"},
		} {
			t.Run(format.format+"/"+source.name, func(t *testing.T) {
				args := []string{"-v", "error", "-f", "lavfi", "-i", source.input, "-t", "1", "-c:a", format.codec}
				if format.format == "mp4" {
					args = append(args, "-movflags", "frag_keyframe+empty_moov")
				}
				args = append(args, "-f", format.format, "pipe:1")
				data, err := exec.Command("ffmpeg", args...).Output()
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := v.Validate(ctx, data, format.mime); err != nil {
					t.Fatalf("nonzero decoded channels must not be treated as silence: %v", err)
				}
			})
		}
	}
}

func TestNonzeroSamples(t *testing.T) {
	for _, tc := range []struct {
		name           string
		bits           []uint32
		found, invalid bool
	}{
		{"positive and negative zero", []uint32{0, 0x80000000, 0}, false, false},
		{"smallest positive subnormal", []uint32{0, 1, 0}, true, false},
		{"smallest negative subnormal", []uint32{0, 0x80000001, 0}, true, false},
		{"positive infinity", []uint32{0x7f800000}, false, true},
		{"negative infinity", []uint32{0xff800000}, false, true},
		{"NaN after signal", []uint32{0x3f800000, 0x7fc00000}, true, true},
		{"negative NaN", []uint32{0xffc00000}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := make([]byte, len(tc.bits)*4)
			for i, bits := range tc.bits {
				binary.LittleEndian.PutUint32(data[i*4:], bits)
			}
			for chunk := 1; chunk <= 7; chunk++ {
				var samples nonzeroSamples
				for start := 0; start < len(data); start += chunk {
					part := data[start:min(start+chunk, len(data))]
					if n, err := samples.Write(part); n != len(part) || err != nil {
						t.Fatalf("write: n=%d err=%v", n, err)
					}
				}
				if samples.found != tc.found || samples.invalid != tc.invalid || samples.used != 0 {
					t.Fatalf("chunk=%d got %+v", chunk, samples)
				}
			}
		})
	}
}
