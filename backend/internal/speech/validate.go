package speech

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	maxProbeOutput = 64 << 10
	maxDiagnostics = 8 << 10
)

type AudioValidator struct {
	ffprobe string
	ffmpeg  string
}

func NewValidator() (*AudioValidator, error) {
	if runtimeGOOS != "linux" {
		return nil, ErrInternal
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, ErrInternal
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, ErrInternal
	}
	return &AudioValidator{ffprobe: ffprobe, ffmpeg: ffmpeg}, nil
}

func (v *AudioValidator) Validate(ctx context.Context, data []byte, contentType string) error {
	if v == nil || v.ffprobe == "" || v.ffmpeg == "" {
		return ErrInternal
	}
	if len(data) == 0 {
		return ErrInvalid
	}
	media, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ErrUnsupported
	}
	demuxer, codec := "", ""
	switch media {
	case "audio/webm":
		demuxer, codec = "matroska", "opus"
		if (params["codecs"] != "" && !strings.EqualFold(params["codecs"], "opus")) || !hasWebMDocType(data) {
			return ErrInvalid
		}
	case "audio/ogg":
		demuxer, codec = "ogg", "opus"
		if (params["codecs"] != "" && !strings.EqualFold(params["codecs"], "opus")) || len(data) < 4 || string(data[:4]) != "OggS" {
			return ErrInvalid
		}
	case "audio/mp4":
		demuxer, codec = "mov", "aac"
		if (params["codecs"] != "" && !strings.EqualFold(params["codecs"], "mp4a.40.2")) || len(data) < 12 || string(data[4:8]) != "ftyp" {
			return ErrInvalid
		}
	default:
		return ErrUnsupported
	}
	for key := range params {
		if key != "codecs" {
			return ErrUnsupported
		}
	}
	file, err := memoryFile(data)
	if err != nil {
		return ErrInternal
	}
	defer file.Close()
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return ErrInternal
	}
	probeArgs := []string{"-v", "error", "-protocol_whitelist", "file,pipe", "-f", demuxer,
		"-show_entries", "stream=codec_type,codec_name,channels,sample_rate", "-of", "json"}
	if media == "audio/mp4" {
		probeArgs = append(probeArgs, "-enable_drefs", "0")
	}
	probeArgs = append(probeArgs, "/proc/self/fd/3")
	probe := exec.CommandContext(ctx, v.ffprobe, probeArgs...)
	probe.ExtraFiles = []*os.File{file}
	probe.WaitDelay = 100 * time.Millisecond
	var output limitedBuffer
	output.limit = maxProbeOutput
	probe.Stdout = &output
	probeDiagnostics := limitedBuffer{limit: maxDiagnostics}
	probe.Stderr = &probeDiagnostics
	if err := probe.Run(); err != nil || output.exceeded || output.data.Len() > maxProbeOutput || probeDiagnostics.data.Len() != 0 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrInvalid
	}
	var info struct {
		Streams []struct {
			CodecType  string `json:"codec_type"`
			CodecName  string `json:"codec_name"`
			Channels   int    `json:"channels"`
			SampleRate string `json:"sample_rate"`
		} `json:"streams"`
	}
	if json.Unmarshal(output.data.Bytes(), &info) != nil || len(info.Streams) != 1 || info.Streams[0].CodecType != "audio" || info.Streams[0].CodecName != codec {
		return ErrInvalid
	}
	channels := info.Streams[0].Channels
	sampleRate, rateErr := strconv.Atoi(info.Streams[0].SampleRate)
	if channels < 1 || channels > 255 || rateErr != nil || sampleRate < 1 || sampleRate > 192000 {
		return ErrInvalid
	}

	// Check samples before timestamp synchronization, which may discard overlapping frames.
	for _, filter := range []string{"asetpts=N/SR/TB", "asetpts=PTS-STARTPTS,aresample=48000:async=1:first_pts=0"} {
		if err := v.validateDecoded(ctx, file, demuxer, media, filter, 0, 0); err != nil {
			return err
		}
	}
	// Inspect every decoded channel separately, before mono mixing and integer rounding.
	return v.validateDecoded(ctx, file, demuxer, media, "asetpts=N/SR/TB", channels, sampleRate)
}

func (v *AudioValidator) validateDecoded(ctx context.Context, file *os.File, demuxer, media, filter string, signalChannels, signalRate int) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return ErrInternal
	}
	decodeArgs := []string{"-hide_banner", "-nostdin", "-v", "warning", "-xerror", "-threads", "1", "-protocol_whitelist", "file,pipe"}
	if media == "audio/mp4" {
		decodeArgs = append(decodeArgs, "-enable_drefs", "0")
	}
	decodeArgs = append(decodeArgs, "-f", demuxer, "-i", "/proc/self/fd/3", "-map", "0:a:0", "-vn", "-sn", "-dn", "-af", filter)
	maxPCMBytes := int64(MaxDuration * 48000 * 2)
	var samples nonzeroSamples
	var sink io.Writer = io.Discard
	if signalChannels == 0 {
		decodeArgs = append(decodeArgs, "-ac", "1", "-ar", "48000", "-f", "s16le", "pipe:1")
	} else {
		decodeArgs = append(decodeArgs, "-f", "f32le", "pipe:1")
		maxPCMBytes = int64(MaxDuration*4) * int64(signalRate) * int64(signalChannels)
		sink = &samples
	}
	decode := exec.CommandContext(ctx, v.ffmpeg, decodeArgs...)
	decode.ExtraFiles = []*os.File{file}
	decode.WaitDelay = 100 * time.Millisecond
	stdout, err := decode.StdoutPipe()
	if err != nil {
		return ErrInternal
	}
	diagnostics := limitedBuffer{limit: maxDiagnostics}
	decode.Stderr = &diagnostics
	if err := decode.Start(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrInternal
	}
	size, readErr := io.Copy(sink, io.LimitReader(stdout, maxPCMBytes+1))
	if readErr != nil || size > maxPCMBytes {
		_ = decode.Process.Kill()
		_ = decode.Wait()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if size > maxPCMBytes {
			return ErrTooLarge
		}
		return ErrInvalid
	}
	waitErr := decode.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil || diagnostics.data.Len() != 0 || size < 2 {
		return ErrInvalid
	}
	if signalChannels > 0 && (!samples.found || samples.invalid || samples.used != 0) {
		return ErrInvalid
	}
	return nil
}

// Reject digital silence without imposing a volume threshold on quiet speech.
type nonzeroSamples struct {
	found, invalid bool
	partial        [4]byte
	used           int
}

func (s *nonzeroSamples) Write(data []byte) (int, error) {
	for _, b := range data {
		s.partial[s.used] = b
		s.used++
		if s.used != len(s.partial) {
			continue
		}
		bits := binary.LittleEndian.Uint32(s.partial[:]) & 0x7fffffff
		s.used = 0
		if bits >= 0x7f800000 {
			s.invalid = true
		} else if bits != 0 {
			s.found = true
		}
	}
	return len(data), nil
}

func hasWebMDocType(data []byte) bool {
	if len(data) < 5 || !bytes.Equal(data[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}) {
		return false
	}
	size, width, ok := ebmlVInt(data[4:], false)
	if !ok || size > 4096 || size > uint64(len(data)-4-width) {
		return false
	}
	header := data[4+width : 4+width+int(size)]
	found := false
	for len(header) > 0 {
		id, n, ok := ebmlVInt(header, true)
		if !ok {
			return false
		}
		header = header[n:]
		length, n, ok := ebmlVInt(header, false)
		if !ok || length > uint64(len(header)-n) {
			return false
		}
		value := header[n : n+int(length)]
		if id == 0x4282 {
			if found || string(value) != "webm" {
				return false
			}
			found = true
		}
		header = header[n+int(length):]
	}
	return found
}

func ebmlVInt(data []byte, keepMarker bool) (uint64, int, bool) {
	if len(data) == 0 || data[0] == 0 {
		return 0, 0, false
	}
	marker, width := byte(0x80), 1
	for data[0]&marker == 0 {
		marker >>= 1
		width++
	}
	if width > len(data) || (keepMarker && width > 4) {
		return 0, 0, false
	}
	value := uint64(data[0])
	if !keepMarker {
		value = uint64(data[0] & (marker - 1))
	}
	for _, b := range data[1:width] {
		value = value<<8 | uint64(b)
	}
	return value, width, true
}

type limitedBuffer struct {
	data     bytes.Buffer
	limit    int
	exceeded bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit + 1 - b.data.Len()
	if remaining <= 0 {
		b.exceeded = true
		return 0, errors.New("output limit")
	}
	if len(p) > remaining {
		_, _ = b.data.Write(p[:remaining])
		b.exceeded = true
		return remaining, errors.New("output limit")
	}
	return b.data.Write(p)
}
