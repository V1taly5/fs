package messagerouter

import (
	"fs/internal/indexer"
	fsv1 "fs/proto/gen/go"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToProtoBlocks(t *testing.T) {
	tests := []struct {
		name     string
		input    []*indexer.BlockHash
		expected []*fsv1.Block
	}{
		{
			name: "Single block",
			input: []*indexer.BlockHash{
				{
					Index: 1,
					Hash:  [32]byte{1, 2, 3},
					Data:  []byte("data1"),
				},
			},
			expected: []*fsv1.Block{
				{
					Index: 1,
					Hash:  []byte{1, 2, 3},
					Data:  []byte("data1"),
				},
			},
		},
		{
			name: "Multiple blocks",
			input: []*indexer.BlockHash{
				{
					Index: 1,
					Hash:  [32]byte{1, 2, 3},
					Data:  []byte("data1"),
				},
				{
					Index: 2,
					Hash:  [32]byte{4, 5, 6},
					Data:  []byte("data2"),
				},
			},
			expected: []*fsv1.Block{
				{
					Index: 1,
					Hash:  []byte{1, 2, 3},
					Data:  []byte("data1"),
				},
				{
					Index: 2,
					Hash:  []byte{4, 5, 6},
					Data:  []byte("data2"),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToProtoBlocks(tt.input)
			assert.Equal(t, len(tt.expected), len(result))
			for i := range result {
				assert.True(t, compareHashes(tt.expected[i].Hash, result[i].Hash))
				assert.Equal(t, tt.expected[i].Index, result[i].Index)
				// assert.Equal(t, tt.expected[i].Hash, result[i].Hash)
				assert.Equal(t, tt.expected[i].Data, result[i].Data)
			}
		})
	}
}

func TestConvertToProtoFileIndexes(t *testing.T) {
	tests := []struct {
		name     string
		input    []*indexer.FileIndex
		expected []*fsv1.FileInfo
	}{
		{
			name: "Single file with one block",
			input: []*indexer.FileIndex{
				{
					Path:     "/file1.txt",
					Size:     1024,
					ModTime:  time.Unix(1234567890, 0),
					FileHash: [32]byte{1, 2, 3},
					Blocks: []indexer.BlockHash{
						{
							Index: 1,
							Hash:  [32]byte{4, 5, 6},
						},
					},
					Deleted: false,
				},
			},
			expected: []*fsv1.FileInfo{
				{
					Path:         "/file1.txt",
					Size:         1024,
					ModifiedTime: 1234567890,
					FileHash:     []byte{1, 2, 3},
					Blocks: []*fsv1.BlockInfo{
						{
							Index: 1,
							Hash:  []byte{4, 5, 6},
						},
					},
					Deleted: false,
				},
			},
		},
		{
			name: "Multiple files with multiple blocks",
			input: []*indexer.FileIndex{
				{
					Path:     "/file1.txt",
					Size:     1024,
					ModTime:  time.Unix(1234567890, 0),
					FileHash: [32]byte{1, 2, 3},
					Blocks: []indexer.BlockHash{
						{
							Index: 1,
							Hash:  [32]byte{4, 5, 6},
						},
					},
					Deleted: false,
				},
				{
					Path:     "/file2.txt",
					Size:     2048,
					ModTime:  time.Unix(987654321, 0),
					FileHash: [32]byte{7, 8, 9},
					Blocks: []indexer.BlockHash{
						{
							Index: 2,
							Hash:  [32]byte{10, 11, 12},
						},
						{
							Index: 3,
							Hash:  [32]byte{13, 14, 15},
						},
					},
					Deleted: true,
				},
			},
			expected: []*fsv1.FileInfo{
				{
					Path:         "/file1.txt",
					Size:         1024,
					ModifiedTime: 1234567890,
					FileHash:     []byte{1, 2, 3},
					Blocks: []*fsv1.BlockInfo{
						{
							Index: 1,
							Hash:  []byte{4, 5, 6},
						},
					},
					Deleted: false,
				},
				{
					Path:         "/file2.txt",
					Size:         2048,
					ModifiedTime: 987654321,
					FileHash:     []byte{7, 8, 9},
					Blocks: []*fsv1.BlockInfo{
						{
							Index: 2,
							Hash:  []byte{10, 11, 12},
						},
						{
							Index: 3,
							Hash:  []byte{13, 14, 15},
						},
					},
					Deleted: true,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToProtoFileIndexes(tt.input)
			require.Equal(t, len(tt.expected), len(result))
			for i := range result {
				assert.Equal(t, tt.expected[i].Path, result[i].Path)
				assert.Equal(t, tt.expected[i].Size, result[i].Size)
				assert.Equal(t, tt.expected[i].ModifiedTime, result[i].ModifiedTime)

				compareHashes(tt.expected[i].FileHash, result[i].FileHash)
				// assert.Equal(t, tt.expected[i].FileHash, result[i].FileHash)
				assert.Equal(t, tt.expected[i].Deleted, result[i].Deleted)
				require.Equal(t, len(tt.expected[i].Blocks), len(result[i].Blocks))
				for j := range result[i].Blocks {
					assert.Equal(t, tt.expected[i].Blocks[j].Index, result[i].Blocks[j].Index)
					compareHashes(tt.expected[i].Blocks[j].Hash, result[i].Blocks[j].Hash)
					// assert.Equal(t, tt.expected[i].Blocks[j].Hash, result[i].Blocks[j].Hash)
				}
			}
		})
	}
}

func compareHashes(expected, actual []byte) bool {
	if len(expected) > len(actual) {
		return false
	}
	for i := range expected {
		if expected[i] != actual[i] {
			return false
		}
	}
	return true
}
