package scrcpy

import (
	"bytes"
	"testing"
)

func TestH264Parser_Parse(t *testing.T) {
	tests := []struct {
		name           string
		input          []byte
		wantCount      int
		wantTypes      []byte
		wantSPS        bool
		wantPPS        bool
		wantIncomplete bool
	}{
		{
			name:           "empty input",
			input:          []byte{},
			wantCount:      0,
			wantIncomplete: true,
		},
		{
			name:           "incomplete start code",
			input:          []byte{0x00, 0x00},
			wantCount:      0,
			wantIncomplete: true,
		},
		{
			name: "single SPS NAL unit",
			input: []byte{
				0x00, 0x00, 0x00, 0x01, // Start code (4 bytes)
				0x67, 0x42, 0x00, 0x1f, 0x96, 0x56, 0x05, 0x01, // SPS data
				0x00, 0x00, 0x00, 0x01, // Next start code
				0x68, // PPS header (to mark end of SPS)
			},
			wantCount: 1,
			wantTypes: []byte{NALTypeSPS},
			wantSPS:   true,
			wantPPS:   false,
		},
		{
			name: "SPS and PPS",
			input: []byte{
				0x00, 0x00, 0x01, // Start code (3 bytes)
				0x67, 0x42, 0x00, 0x1f, // SPS
				0x00, 0x00, 0x01, // Start code
				0x68, 0xce, 0x3c, 0x80, // PPS
				0x00, 0x00, 0x01, // Next start code (marks end)
				0x65, // IDR slice
			},
			wantCount: 2,
			wantTypes: []byte{NALTypeSPS, NALTypePPS},
			wantSPS:   true,
			wantPPS:   true,
		},
		{
			name: "IDR frame",
			input: []byte{
				0x00, 0x00, 0x00, 0x01,
				0x65, 0x88, 0x84, 0x00, // IDR slice
				0x00, 0x00, 0x00, 0x01,
				0x41, // Non-IDR slice
			},
			wantCount: 1,
			wantTypes: []byte{NALTypeSliceIDR},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewH264Parser()
			parser.Write(tt.input)

			units, err := parser.Parse()
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			if len(units) != tt.wantCount {
				t.Errorf("Parse() got %d units, want %d", len(units), tt.wantCount)
			}

			for i, unit := range units {
				if i < len(tt.wantTypes) && unit.Type != tt.wantTypes[i] {
					t.Errorf("unit[%d].Type = %d, want %d", i, unit.Type, tt.wantTypes[i])
				}
			}

			if tt.wantSPS && parser.SPS() == nil {
				t.Error("expected SPS to be cached")
			}
			if tt.wantPPS && parser.PPS() == nil {
				t.Error("expected PPS to be cached")
			}
		})
	}
}

func TestNALUnit_IsKeyframe(t *testing.T) {
	tests := []struct {
		nalType  byte
		expected bool
	}{
		{NALTypeSliceIDR, true},
		{NALTypeSliceNonIDR, false},
		{NALTypeSPS, false},
		{NALTypePPS, false},
	}

	for _, tt := range tests {
		unit := &NALUnit{Type: tt.nalType}
		if got := unit.IsKeyframe(); got != tt.expected {
			t.Errorf("NALUnit{Type: %d}.IsKeyframe() = %v, want %v", tt.nalType, got, tt.expected)
		}
	}
}

func TestNALUnit_IsSPS(t *testing.T) {
	spsUnit := &NALUnit{Type: NALTypeSPS}
	if !spsUnit.IsSPS() {
		t.Error("expected IsSPS() to return true for SPS NAL")
	}

	nonSpsUnit := &NALUnit{Type: NALTypeSliceIDR}
	if nonSpsUnit.IsSPS() {
		t.Error("expected IsSPS() to return false for non-SPS NAL")
	}
}

func TestNALUnit_IsPPS(t *testing.T) {
	ppsUnit := &NALUnit{Type: NALTypePPS}
	if !ppsUnit.IsPPS() {
		t.Error("expected IsPPS() to return true for PPS NAL")
	}

	nonPpsUnit := &NALUnit{Type: NALTypeSPS}
	if nonPpsUnit.IsPPS() {
		t.Error("expected IsPPS() to return false for non-PPS NAL")
	}
}

func TestH264Parser_HasConfig(t *testing.T) {
	parser := NewH264Parser()

	// Initially no config
	if parser.HasConfig() {
		t.Error("expected HasConfig() to return false initially")
	}

	// Add SPS and PPS
	input := []byte{
		0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1f, // SPS
		0x00, 0x00, 0x01, 0x68, 0xce, 0x3c, 0x80, // PPS
		0x00, 0x00, 0x01, 0x65, // End marker
	}
	parser.Write(input)
	_, _ = parser.Parse()

	if !parser.HasConfig() {
		t.Error("expected HasConfig() to return true after SPS/PPS")
	}
}

func TestH264Parser_Reset(t *testing.T) {
	parser := NewH264Parser()

	// Add some data and SPS/PPS
	input := []byte{
		0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1f,
		0x00, 0x00, 0x01, 0x68, 0xce, 0x3c, 0x80,
		0x00, 0x00, 0x01, 0x65,
	}
	parser.Write(input)
	_, _ = parser.Parse()

	// Reset
	parser.Reset()

	if parser.SPS() != nil {
		t.Error("expected SPS to be nil after reset")
	}
	if parser.PPS() != nil {
		t.Error("expected PPS to be nil after reset")
	}
	if parser.HasConfig() {
		t.Error("expected HasConfig() to return false after reset")
	}
}

func TestFindStartCode(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected int
	}{
		{
			name:     "no start code",
			input:    []byte{0x01, 0x02, 0x03, 0x04},
			expected: -1,
		},
		{
			name:     "3-byte start code at beginning",
			input:    []byte{0x00, 0x00, 0x01, 0x67},
			expected: 0,
		},
		{
			name:     "3-byte start code in middle",
			input:    []byte{0xFF, 0xFF, 0x00, 0x00, 0x01, 0x67},
			expected: 2,
		},
		{
			name:     "4-byte start code",
			input:    []byte{0x00, 0x00, 0x00, 0x01, 0x67},
			expected: 1, // Points to the second 0x00 of the 0x00 0x00 0x01 sequence
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findStartCode(tt.input)
			if got != tt.expected {
				t.Errorf("findStartCode() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestCreateAVCDecoderConfigurationRecord(t *testing.T) {
	sps := []byte{0x67, 0x42, 0x00, 0x1f, 0x96, 0x56, 0x05, 0x01}
	pps := []byte{0x68, 0xce, 0x3c, 0x80}

	record, err := CreateAVCDecoderConfigurationRecord(sps, pps)
	if err != nil {
		t.Fatalf("CreateAVCDecoderConfigurationRecord() error = %v", err)
	}

	// Check structure
	if record[0] != 0x01 {
		t.Error("configuration version should be 1")
	}
	if record[1] != sps[1] {
		t.Error("AVCProfileIndication should match SPS byte 1")
	}
	if record[2] != sps[2] {
		t.Error("profile_compatibility should match SPS byte 2")
	}
	if record[3] != sps[3] {
		t.Error("AVCLevelIndication should match SPS byte 3")
	}
	if record[4] != 0xFF {
		t.Error("lengthSizeMinusOne should be 0xFF (3)")
	}
	if record[5] != 0xE1 {
		t.Error("numOfSequenceParameterSets should be 0xE1 (1)")
	}
}

func TestCreateAVCDecoderConfigurationRecord_Errors(t *testing.T) {
	// SPS too short
	_, err := CreateAVCDecoderConfigurationRecord([]byte{0x67, 0x42, 0x00}, []byte{0x68})
	if err == nil {
		t.Error("expected error for short SPS")
	}

	// Empty PPS
	_, err = CreateAVCDecoderConfigurationRecord([]byte{0x67, 0x42, 0x00, 0x1f}, []byte{})
	if err == nil {
		t.Error("expected error for empty PPS")
	}
}

func TestNALUnitToAnnexB(t *testing.T) {
	nalu := []byte{0x67, 0x42, 0x00, 0x1f}
	result := NALUnitToAnnexB(nalu)

	// Should have 4-byte start code prefix
	if !bytes.HasPrefix(result, StartCode4) {
		t.Error("expected 4-byte start code prefix")
	}

	// Data should follow
	if !bytes.HasSuffix(result, nalu) {
		t.Error("expected NAL unit data after start code")
	}

	if len(result) != 4+len(nalu) {
		t.Errorf("expected length %d, got %d", 4+len(nalu), len(result))
	}
}

func TestScrcpyVersion_Compare(t *testing.T) {
	tests := []struct {
		v1, v2   ScrcpyVersion
		expected int
	}{
		{ScrcpyVersion{2, 0, 0}, ScrcpyVersion{2, 0, 0}, 0},
		{ScrcpyVersion{2, 0, 0}, ScrcpyVersion{1, 9, 0}, 1},
		{ScrcpyVersion{1, 9, 0}, ScrcpyVersion{2, 0, 0}, -1},
		{ScrcpyVersion{2, 1, 0}, ScrcpyVersion{2, 0, 0}, 1},
		{ScrcpyVersion{2, 0, 1}, ScrcpyVersion{2, 0, 0}, 1},
	}

	for _, tt := range tests {
		got := tt.v1.Compare(tt.v2)
		if got != tt.expected {
			t.Errorf("%v.Compare(%v) = %d, want %d", tt.v1, tt.v2, got, tt.expected)
		}
	}
}

func TestScrcpyVersion_SupportsAudio(t *testing.T) {
	tests := []struct {
		version  ScrcpyVersion
		expected bool
	}{
		{ScrcpyVersion{2, 0, 0}, true},
		{ScrcpyVersion{2, 4, 0}, true},
		{ScrcpyVersion{1, 25, 0}, false},
		{ScrcpyVersion{1, 99, 99}, false},
	}

	for _, tt := range tests {
		if got := tt.version.SupportsAudio(); got != tt.expected {
			t.Errorf("%v.SupportsAudio() = %v, want %v", tt.version, got, tt.expected)
		}
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected ScrcpyVersion
		wantErr  bool
	}{
		{"2.4", ScrcpyVersion{2, 4, 0}, false},
		{"2.4.1", ScrcpyVersion{2, 4, 1}, false},
		{"1.25", ScrcpyVersion{1, 25, 0}, false},
		{"2", ScrcpyVersion{}, true},
		{"invalid", ScrcpyVersion{}, true},
	}

	for _, tt := range tests {
		got, err := parseVersion(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseVersion(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.expected {
			t.Errorf("parseVersion(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestBitReader(t *testing.T) {
	data := []byte{0b10110010, 0b01001101}
	reader := newBitReader(data)

	// Read 4 bits: should be 1011 = 11
	val, err := reader.readBits(4)
	if err != nil {
		t.Fatalf("readBits(4) error = %v", err)
	}
	if val != 11 {
		t.Errorf("readBits(4) = %d, want 11", val)
	}

	// Read 4 more bits: should be 0010 = 2
	val, err = reader.readBits(4)
	if err != nil {
		t.Fatalf("readBits(4) error = %v", err)
	}
	if val != 2 {
		t.Errorf("readBits(4) = %d, want 2", val)
	}

	// Read 8 more bits: should be 01001101 = 77
	val, err = reader.readBits(8)
	if err != nil {
		t.Fatalf("readBits(8) error = %v", err)
	}
	if val != 77 {
		t.Errorf("readBits(8) = %d, want 77", val)
	}

	// Reading more should fail
	_, err = reader.readBits(1)
	if err == nil {
		t.Error("expected error when reading beyond buffer")
	}
}

func TestReadUEG(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected uint32
	}{
		{"0 (binary: 1)", []byte{0b10000000}, 0},
		{"1 (binary: 010)", []byte{0b01000000}, 1},
		{"2 (binary: 011)", []byte{0b01100000}, 2},
		{"3 (binary: 00100)", []byte{0b00100000}, 3},
		{"4 (binary: 00101)", []byte{0b00101000}, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := newBitReader(tt.data)
			got, err := reader.readUEG()
			if err != nil {
				t.Fatalf("readUEG() error = %v", err)
			}
			if got != tt.expected {
				t.Errorf("readUEG() = %d, want %d", got, tt.expected)
			}
		})
	}
}
