package paths

import (
	"path/filepath"
	"testing"

	"github.com/zk-org/zk/internal/util"
	"github.com/zk-org/zk/internal/util/fixtures"
	"github.com/zk-org/zk/internal/util/test/assert"
)

func testWalkFunc(t *testing.T, path string, walkFunc func(string, util.Logger, string, func(string) (bool, error)) <-chan Metadata) {
	shouldIgnore := func(path string) (bool, error) {
		return filepath.Ext(path) != ".md", nil
	}

	notebookRoot := filepath.Base(path)
	actual := make([]string, 0)
	for m := range walkFunc(path, &util.NullLogger, notebookRoot, shouldIgnore) {
		assert.NotNil(t, m.Modified)
		actual = append(actual, m.Path)
	}

	assert.Equal(t, actual, []string{
		"Dir3/a.md",
		"a.md",
		"b.md",
		"dir1/a.md",
		"dir1/b.md",
		"dir1/dir1/a.md",
		"dir1 a space/a.md",
		"dir2/a.md",
	})
}

func TestWalk(t *testing.T) {
	var path = fixtures.Path("walk")
	testWalkFunc(t, path, Walk)
}

// Walk should ignore all hidden files and dirs (prefixed with "."), with
// exception of the notebook's root dir; i.e the root dir is allowed to be
// hidden.
func TestWalkHidden(t *testing.T) {
	var path = fixtures.Path(".walk-hidden")
	testWalkFunc(t, path, Walk)
}

// ParallelWalk should return files in the same order as Walk
func TestParallelWalk(t *testing.T) {
	var path = fixtures.Path("walk")
	testWalkFunc(t, path, ParallelWalk)
}

// ParallelWalk should ignore all hidden files in the same way as Walk
func TestParallelWalkHidden(t *testing.T) {
	var path = fixtures.Path(".walk-hidden")
	testWalkFunc(t, path, ParallelWalk)
}

func TestPathSorting(t *testing.T) {
	assert.True(t, comparePaths("a", "b"))
	assert.False(t, comparePaths("b", "a"))
	assert.True(t, comparePaths("aa", "b"))
	assert.False(t, comparePaths("b", "aa"))
	assert.True(t, comparePaths("a/a", "b"))
	assert.False(t, comparePaths("b", "a/a"))
	assert.True(t, comparePaths("a/a", "b+/a"))
	assert.False(t, comparePaths("b+/a", "a/a"))
	assert.True(t, comparePaths("aa/a", "ba/a"))
	assert.False(t, comparePaths("ba/a", "aa/a"))
	assert.True(t, comparePaths("aa/bb/cc/file.md", "aa/bb/cc+/file.md"))
	assert.False(t, comparePaths("aa/bb/cc+/file.md", "aa/bb/cc/file.md"))
	assert.True(t, comparePaths("aa/bb/cc/file.md", "aa/bb/cc+.md"))
	assert.False(t, comparePaths("aa/bb/cc+.md", "aa/bb/cc/file.md"))
}
