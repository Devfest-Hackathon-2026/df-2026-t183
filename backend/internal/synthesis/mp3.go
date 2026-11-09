package synthesis

// hasMP3Frames checks the complete frame layout before the separate ffmpeg decode.
func hasMP3Frames(audio []byte) bool {
	if len(audio) >= 3 && string(audio[:3]) == "ID3" {
		if len(audio) < 10 || audio[3] < 2 || audio[3] > 4 {
			return false
		}
		size := 0
		for _, b := range audio[6:10] {
			if b&0x80 != 0 {
				return false
			}
			size = size<<7 | int(b)
		}
		size += 10
		if audio[3] == 4 && audio[5]&0x10 != 0 {
			size += 10
		}
		if size > len(audio) {
			return false
		}
		audio = audio[size:]
	}
	frames := 0
	for len(audio) > 0 {
		if len(audio) == 128 && string(audio[:3]) == "TAG" {
			return frames > 0
		}
		if len(audio) < 4 || audio[0] != 0xff || audio[1]&0xe0 != 0xe0 {
			return false
		}
		version, layer := (audio[1]>>3)&3, (audio[1]>>1)&3
		bitrate, rate := (audio[2]>>4)&15, (audio[2]>>2)&3
		if version == 1 || layer != 1 || bitrate == 0 || bitrate == 15 || rate == 3 {
			return false
		}
		rates := [...]int{44100, 48000, 32000}
		bitrates := [...]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
		coefficient := 144000
		sampleRate := rates[rate]
		if version != 3 {
			bitrates = [...]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}
			coefficient = 72000
			sampleRate /= 2
			if version == 0 {
				sampleRate /= 2
			}
		}
		size := coefficient*bitrates[bitrate]/sampleRate + int((audio[2]>>1)&1)
		if size < 4 || size > len(audio) {
			return false
		}
		audio = audio[size:]
		frames++
	}
	return frames > 0
}
