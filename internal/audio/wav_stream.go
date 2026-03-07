package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/disgoorg/disgo/voice"
	"github.com/kazzmir/opus-go/opus"
)

const pcmFormat = 1

type WAVStream struct {
	encoder    *opus.Encoder
	sourceRate int
	step       float64
	sourcePos  float64
	left       []int16
	right      []int16
	opusBuf    []byte
}

func NewWAVStream(path string) (*WAVStream, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return NewWAVStreamFromReader(file)
}

func NewWAVStreamFromReader(r io.Reader) (*WAVStream, error) {
	sourceRate, left, right, err := decodeWAV(r)
	if err != nil {
		return nil, err
	}
	if len(left) == 0 || len(right) == 0 {
		return nil, errors.New("wav data is empty")
	}

	encoder, err := opus.NewEncoder(DiscordSampleRate, AudioChannels, opus.ApplicationAudio)
	if err != nil {
		return nil, err
	}

	return &WAVStream{
		encoder:    encoder,
		sourceRate: sourceRate,
		step:       float64(sourceRate) / float64(DiscordSampleRate),
		left:       left,
		right:      right,
		opusBuf:    make([]byte, MaxOpusPacketSize),
	}, nil
}

func (s *WAVStream) Close() error {
	if s.encoder == nil {
		return nil
	}
	return s.encoder.Close()
}

func (s *WAVStream) NextOpusFrame() ([]byte, error) {
	pcm := make([]int16, voice.OpusFrameSize*AudioChannels)
	filled := 0

	for filled < voice.OpusFrameSize {
		left, right, ok := s.nextSample()
		if !ok {
			if filled == 0 {
				return nil, io.EOF
			}
			break
		}

		pcm[filled*AudioChannels] = left
		pcm[filled*AudioChannels+1] = right
		filled++
	}

	n, err := s.encoder.Encode(pcm, voice.OpusFrameSize, s.opusBuf)
	if err != nil {
		return nil, err
	}
	return s.opusBuf[:n], nil
}

func (s *WAVStream) nextSample() (int16, int16, bool) {
	index := int(math.Floor(s.sourcePos))
	if index < 0 || index >= len(s.left) {
		return 0, 0, false
	}

	fraction := s.sourcePos - float64(index)
	nextIndex := index + 1
	if nextIndex >= len(s.left) {
		nextIndex = index
	}

	left := lerpInt16(s.left[index], s.left[nextIndex], fraction)
	right := lerpInt16(s.right[index], s.right[nextIndex], fraction)
	s.sourcePos += s.step

	return left, right, true
}

func decodeWAV(r io.Reader) (int, []int16, []int16, error) {
	header := make([]byte, 12)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, nil, fmt.Errorf("read wav header: %w", err)
	}
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return 0, nil, nil, errors.New("invalid wav header")
	}

	var (
		audioFormat   uint16
		channelCount  uint16
		sampleRate    uint32
		bitsPerSample uint16
		dataChunk     []byte
		fmtFound      bool
		dataFound     bool
	)

	for {
		chunkHeader := make([]byte, 8)
		if _, err := io.ReadFull(r, chunkHeader); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return 0, nil, nil, fmt.Errorf("read wav chunk header: %w", err)
		}

		chunkID := string(chunkHeader[0:4])
		chunkSize := binary.LittleEndian.Uint32(chunkHeader[4:8])
		chunkData := make([]byte, chunkSize)
		if _, err := io.ReadFull(r, chunkData); err != nil {
			return 0, nil, nil, fmt.Errorf("read wav chunk %s: %w", chunkID, err)
		}
		if chunkSize%2 == 1 {
			if _, err := io.CopyN(io.Discard, r, 1); err != nil {
				return 0, nil, nil, fmt.Errorf("skip wav padding: %w", err)
			}
		}

		switch chunkID {
		case "fmt ":
			if len(chunkData) < 16 {
				return 0, nil, nil, errors.New("wav fmt chunk too short")
			}
			audioFormat = binary.LittleEndian.Uint16(chunkData[0:2])
			channelCount = binary.LittleEndian.Uint16(chunkData[2:4])
			sampleRate = binary.LittleEndian.Uint32(chunkData[4:8])
			bitsPerSample = binary.LittleEndian.Uint16(chunkData[14:16])
			fmtFound = true
		case "data":
			dataChunk = chunkData
			dataFound = true
		}
	}

	if !fmtFound {
		return 0, nil, nil, errors.New("wav fmt chunk not found")
	}
	if !dataFound {
		return 0, nil, nil, errors.New("wav data chunk not found")
	}
	if audioFormat != pcmFormat {
		return 0, nil, nil, fmt.Errorf("unsupported wav format: %d", audioFormat)
	}
	if channelCount != 1 && channelCount != 2 {
		return 0, nil, nil, fmt.Errorf("unsupported wav channel count: %d", channelCount)
	}
	if bitsPerSample != 16 {
		return 0, nil, nil, fmt.Errorf("unsupported wav bits per sample: %d", bitsPerSample)
	}
	if sampleRate == 0 {
		return 0, nil, nil, errors.New("invalid wav sample rate")
	}

	frameSize := int(channelCount) * BytesPerSample
	if frameSize == 0 || len(dataChunk) < frameSize {
		return 0, nil, nil, errors.New("invalid wav frame data")
	}

	sampleCount := len(dataChunk) / frameSize
	left := make([]int16, 0, sampleCount)
	right := make([]int16, 0, sampleCount)

	for i := 0; i+frameSize <= len(dataChunk); i += frameSize {
		leftSample := int16(binary.LittleEndian.Uint16(dataChunk[i : i+BytesPerSample]))
		if channelCount == 1 {
			left = append(left, leftSample)
			right = append(right, leftSample)
			continue
		}

		rightOffset := i + BytesPerSample
		rightSample := int16(binary.LittleEndian.Uint16(dataChunk[rightOffset : rightOffset+BytesPerSample]))
		left = append(left, leftSample)
		right = append(right, rightSample)
	}

	return int(sampleRate), left, right, nil
}

func lerpInt16(a int16, b int16, fraction float64) int16 {
	if fraction <= 0 {
		return a
	}
	if fraction >= 1 {
		return b
	}

	value := float64(a) + (float64(b)-float64(a))*fraction
	if value > math.MaxInt16 {
		value = math.MaxInt16
	}
	if value < math.MinInt16 {
		value = math.MinInt16
	}
	return int16(math.Round(value))
}
