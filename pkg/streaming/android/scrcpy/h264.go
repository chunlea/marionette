package scrcpy

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// H.264 NAL unit types relevant for streaming.
const (
	// NALTypeSliceNonIDR is a non-IDR slice (P/B frame).
	NALTypeSliceNonIDR = 1
	// NALTypeSlicePartA is a slice data partition A.
	NALTypeSlicePartA = 2
	// NALTypeSlicePartB is a slice data partition B.
	NALTypeSlicePartB = 3
	// NALTypeSlicePartC is a slice data partition C.
	NALTypeSlicePartC = 4
	// NALTypeSliceIDR is an IDR slice (keyframe).
	NALTypeSliceIDR = 5
	// NALTypeSEI is supplemental enhancement information.
	NALTypeSEI = 6
	// NALTypeSPS is sequence parameter set.
	NALTypeSPS = 7
	// NALTypePPS is picture parameter set.
	NALTypePPS = 8
	// NALTypeAUD is access unit delimiter.
	NALTypeAUD = 9
	// NALTypeEOSeq is end of sequence.
	NALTypeEOSeq = 10
	// NALTypeEOStream is end of stream.
	NALTypeEOStream = 11
	// NALTypeFiller is filler data.
	NALTypeFiller = 12
)

// Annex B start codes.
var (
	StartCode3 = []byte{0x00, 0x00, 0x01}
	StartCode4 = []byte{0x00, 0x00, 0x00, 0x01}
)

// NALUnit represents a parsed H.264 NAL unit.
type NALUnit struct {
	// Type is the NAL unit type (lower 5 bits of first byte).
	Type byte

	// RefIDC is the nal_ref_idc field (bits 5-6 of first byte).
	RefIDC byte

	// Data is the raw NAL unit data including the header byte.
	Data []byte
}

// IsKeyframe returns true if this NAL unit is part of a keyframe.
func (n *NALUnit) IsKeyframe() bool {
	return n.Type == NALTypeSliceIDR
}

// IsSPS returns true if this is a Sequence Parameter Set.
func (n *NALUnit) IsSPS() bool {
	return n.Type == NALTypeSPS
}

// IsPPS returns true if this is a Picture Parameter Set.
func (n *NALUnit) IsPPS() bool {
	return n.Type == NALTypePPS
}

// IsSlice returns true if this NAL unit contains slice data.
func (n *NALUnit) IsSlice() bool {
	return n.Type >= NALTypeSliceNonIDR && n.Type <= NALTypeSliceIDR
}

// H264Parser parses H.264 Annex B byte streams.
type H264Parser struct {
	// buffer accumulates incoming data.
	buffer []byte

	// sps is the last parsed SPS NAL unit.
	sps []byte

	// pps is the last parsed PPS NAL unit.
	pps []byte
}

// NewH264Parser creates a new H.264 parser.
func NewH264Parser() *H264Parser {
	return &H264Parser{
		buffer: make([]byte, 0, 1024*1024), // 1MB initial capacity
	}
}

// Write adds data to the parser buffer.
func (p *H264Parser) Write(data []byte) {
	p.buffer = append(p.buffer, data...)
}

// Parse extracts complete NAL units from the buffer.
// Returns a slice of NAL units found and removes them from the buffer.
func (p *H264Parser) Parse() ([]NALUnit, error) {
	var units []NALUnit

	for {
		unit, remaining, err := p.extractNextNAL()
		if err != nil {
			if errors.Is(err, errIncompleteNAL) {
				// Need more data
				break
			}
			return units, err
		}
		if unit == nil {
			break
		}

		// Update buffer to remaining data
		p.buffer = remaining

		// Track SPS/PPS
		if unit.IsSPS() {
			p.sps = make([]byte, len(unit.Data))
			copy(p.sps, unit.Data)
		} else if unit.IsPPS() {
			p.pps = make([]byte, len(unit.Data))
			copy(p.pps, unit.Data)
		}

		units = append(units, *unit)
	}

	return units, nil
}

// SPS returns the last parsed SPS NAL unit, or nil if none seen.
func (p *H264Parser) SPS() []byte {
	return p.sps
}

// PPS returns the last parsed PPS NAL unit, or nil if none seen.
func (p *H264Parser) PPS() []byte {
	return p.pps
}

// HasConfig returns true if both SPS and PPS have been seen.
func (p *H264Parser) HasConfig() bool {
	return len(p.sps) > 0 && len(p.pps) > 0
}

// Reset clears the parser buffer and cached SPS/PPS.
func (p *H264Parser) Reset() {
	p.buffer = p.buffer[:0]
	p.sps = nil
	p.pps = nil
}

// errIncompleteNAL indicates more data is needed to parse a complete NAL.
var errIncompleteNAL = errors.New("incomplete NAL unit")

// extractNextNAL extracts the next NAL unit from the buffer.
// Returns the NAL unit, remaining buffer, and any error.
func (p *H264Parser) extractNextNAL() (*NALUnit, []byte, error) {
	if len(p.buffer) < 4 {
		return nil, p.buffer, errIncompleteNAL
	}

	// Find start code
	startIdx := findStartCode(p.buffer)
	if startIdx < 0 {
		return nil, p.buffer, errIncompleteNAL
	}

	// Determine start code length (3 or 4 bytes)
	startCodeLen := 3
	if startIdx > 0 && p.buffer[startIdx-1] == 0x00 {
		startIdx--
		startCodeLen = 4
	}

	// Skip start code to get to NAL unit
	nalStart := startIdx + startCodeLen
	if nalStart >= len(p.buffer) {
		return nil, p.buffer, errIncompleteNAL
	}

	// Find next start code or end of buffer
	remaining := p.buffer[nalStart:]
	nextStartIdx := findStartCode(remaining)

	var nalData []byte
	var newRemaining []byte

	if nextStartIdx < 0 {
		// No more start codes found - need more data
		return nil, p.buffer, errIncompleteNAL
	}

	// Adjust for 4-byte start code
	if nextStartIdx > 0 && remaining[nextStartIdx-1] == 0x00 {
		nextStartIdx--
	}

	nalData = remaining[:nextStartIdx]
	newRemaining = remaining[nextStartIdx:]

	// Remove any trailing zeros from NAL data
	nalData = bytes.TrimRight(nalData, "\x00")

	if len(nalData) == 0 {
		return nil, newRemaining, nil
	}

	// Parse NAL header
	header := nalData[0]
	unit := &NALUnit{
		Type:   header & 0x1F,
		RefIDC: (header >> 5) & 0x03,
		Data:   nalData,
	}

	return unit, newRemaining, nil
}

// findStartCode finds the index of the first 0x000001 sequence in data.
// Returns -1 if not found.
func findStartCode(data []byte) int {
	for i := 0; i < len(data)-2; i++ {
		if data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x01 {
			return i
		}
	}
	return -1
}

// ExtractSPSPPS extracts SPS and PPS from a raw H.264 stream.
// Returns SPS data, PPS data, and any error.
func ExtractSPSPPS(data []byte) (sps, pps []byte, err error) {
	parser := NewH264Parser()
	parser.Write(data)

	units, err := parser.Parse()
	if err != nil {
		return nil, nil, err
	}

	for _, unit := range units {
		if unit.IsSPS() && sps == nil {
			sps = make([]byte, len(unit.Data))
			copy(sps, unit.Data)
		} else if unit.IsPPS() && pps == nil {
			pps = make([]byte, len(unit.Data))
			copy(pps, unit.Data)
		}
		if sps != nil && pps != nil {
			break
		}
	}

	return sps, pps, nil
}

// ParseSPSDimensions extracts width and height from an SPS NAL unit.
// This is a simplified parser that handles common cases.
func ParseSPSDimensions(sps []byte) (width, height int, err error) {
	if len(sps) < 4 {
		return 0, 0, errors.New("SPS too short")
	}

	// Skip NAL header
	data := sps[1:]

	// Create bit reader
	reader := newBitReader(data)

	// profile_idc
	_, err = reader.readBits(8)
	if err != nil {
		return 0, 0, err
	}

	// constraint_set flags + reserved + level_idc
	_, err = reader.readBits(16)
	if err != nil {
		return 0, 0, err
	}

	// seq_parameter_set_id
	_, err = reader.readUEG()
	if err != nil {
		return 0, 0, err
	}

	// For high profiles, read additional fields
	// This is a simplified version - a complete parser would handle all profiles

	// log2_max_frame_num_minus4
	_, err = reader.readUEG()
	if err != nil {
		return 0, 0, err
	}

	// pic_order_cnt_type
	picOrderCntType, err := reader.readUEG()
	if err != nil {
		return 0, 0, err
	}

	if picOrderCntType == 0 {
		// log2_max_pic_order_cnt_lsb_minus4
		_, err = reader.readUEG()
		if err != nil {
			return 0, 0, err
		}
	} else if picOrderCntType == 1 {
		// Skip complex pic_order_cnt_type == 1 fields
		return 0, 0, errors.New("pic_order_cnt_type 1 not supported")
	}

	// max_num_ref_frames
	_, err = reader.readUEG()
	if err != nil {
		return 0, 0, err
	}

	// gaps_in_frame_num_value_allowed_flag
	_, err = reader.readBits(1)
	if err != nil {
		return 0, 0, err
	}

	// pic_width_in_mbs_minus1
	picWidthInMbsMinus1, err := reader.readUEG()
	if err != nil {
		return 0, 0, err
	}

	// pic_height_in_map_units_minus1
	picHeightInMapUnitsMinus1, err := reader.readUEG()
	if err != nil {
		return 0, 0, err
	}

	// frame_mbs_only_flag
	frameMbsOnlyFlag, err := reader.readBits(1)
	if err != nil {
		return 0, 0, err
	}

	// Calculate dimensions
	width = int((picWidthInMbsMinus1 + 1) * 16)
	height = int((picHeightInMapUnitsMinus1 + 1) * 16)

	// If not frame_mbs_only, multiply height by 2
	if frameMbsOnlyFlag == 0 {
		height *= 2
	}

	return width, height, nil
}

// bitReader reads bits from a byte slice.
type bitReader struct {
	data   []byte
	offset int // bit offset
	length int // total bits
}

// newBitReader creates a new bit reader.
func newBitReader(data []byte) *bitReader {
	return &bitReader{
		data:   data,
		offset: 0,
		length: len(data) * 8,
	}
}

// readBits reads n bits and returns as uint32.
func (r *bitReader) readBits(n int) (uint32, error) {
	if r.offset+n > r.length {
		return 0, errors.New("not enough bits")
	}

	var result uint32
	for i := 0; i < n; i++ {
		byteIdx := (r.offset + i) / 8
		bitIdx := 7 - ((r.offset + i) % 8)
		bit := (r.data[byteIdx] >> bitIdx) & 0x01
		result = (result << 1) | uint32(bit)
	}

	r.offset += n
	return result, nil
}

// readUEG reads an unsigned Exp-Golomb coded value.
func (r *bitReader) readUEG() (uint32, error) {
	// Count leading zeros
	leadingZeros := 0
	for {
		bit, err := r.readBits(1)
		if err != nil {
			return 0, err
		}
		if bit == 1 {
			break
		}
		leadingZeros++
		if leadingZeros > 32 {
			return 0, errors.New("invalid exp-golomb code")
		}
	}

	if leadingZeros == 0 {
		return 0, nil
	}

	// Read the remaining bits
	remaining, err := r.readBits(leadingZeros)
	if err != nil {
		return 0, err
	}

	return (1 << leadingZeros) - 1 + remaining, nil
}

// CreateAVCDecoderConfigurationRecord creates an AVC decoder configuration record
// from SPS and PPS NAL units. This is used in MP4 containers and some WebRTC implementations.
func CreateAVCDecoderConfigurationRecord(sps, pps []byte) ([]byte, error) {
	if len(sps) < 4 {
		return nil, errors.New("SPS too short")
	}
	if len(pps) == 0 {
		return nil, errors.New("PPS is empty")
	}

	buf := bytes.NewBuffer(make([]byte, 0, 128))

	// configurationVersion
	buf.WriteByte(0x01)

	// AVCProfileIndication (from SPS byte 1)
	buf.WriteByte(sps[1])

	// profile_compatibility (from SPS byte 2)
	buf.WriteByte(sps[2])

	// AVCLevelIndication (from SPS byte 3)
	buf.WriteByte(sps[3])

	// lengthSizeMinusOne (3 = 4-byte NAL length prefix)
	buf.WriteByte(0xFF)

	// numOfSequenceParameterSets
	buf.WriteByte(0xE1) // 1 SPS

	// SPS length (2 bytes, big endian)
	spsLen := uint16(len(sps))
	_ = binary.Write(buf, binary.BigEndian, spsLen)

	// SPS data
	buf.Write(sps)

	// numOfPictureParameterSets
	buf.WriteByte(0x01) // 1 PPS

	// PPS length (2 bytes, big endian)
	ppsLen := uint16(len(pps))
	_ = binary.Write(buf, binary.BigEndian, ppsLen)

	// PPS data
	buf.Write(pps)

	return buf.Bytes(), nil
}

// NALUnitToAnnexB converts a length-prefixed NAL unit to Annex B format.
// Adds the 4-byte start code prefix.
func NALUnitToAnnexB(nalu []byte) []byte {
	result := make([]byte, 4+len(nalu))
	copy(result[:4], StartCode4)
	copy(result[4:], nalu)
	return result
}

// AnnexBToAVCC converts Annex B format to AVCC format (4-byte length prefix).
// This is useful for packetizing H.264 for certain containers.
func AnnexBToAVCC(annexB []byte) []byte {
	parser := NewH264Parser()
	parser.Write(annexB)

	units, _ := parser.Parse()
	if len(units) == 0 {
		return nil
	}

	var result bytes.Buffer
	for _, unit := range units {
		// Write 4-byte length prefix (big endian)
		length := uint32(len(unit.Data))
		_ = binary.Write(&result, binary.BigEndian, length)
		result.Write(unit.Data)
	}

	return result.Bytes()
}
