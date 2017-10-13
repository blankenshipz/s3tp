package main

import (
  "fmt"
  "io"
  "net"
  "os"
  "testing"

  "github.com/pkg/sftp"
  "github.com/stretchr/testify/assert"
)

// tests
func TestRequestReaddir(t *testing.T) {
  p := clientRequestServerPair(t)
  defer p.Close()

  // list buckets
  files, err := p.cli.ReadDir("/")
  assert.Nil(t, err)
  names := []string{files[1].Name()}
  assert.Equal(t, []string{"s3tp-test"}, names)

  // list files one level deep in the bucket
  files, err = p.cli.ReadDir("/s3tp-test")
  assert.Nil(t, err)
  names = []string{files[0].Name(), files[1].Name()}
  assert.Equal(t, []string{"dir-1-deep", "hello"}, names)

  // list files two levels deep in the bucket
  files, err = p.cli.ReadDir("/s3tp-test/dir-1-deep")
  assert.Nil(t, err)
  names = []string{files[0].Name(), files[1].Name()}
  assert.Equal(t, []string{"dir-2-deep", "lux.png"}, names)

  // // list files three levels deep in the bucket
  // files, err = p.cli.ReadDir("/s3tp-test/dir-1-deep/dir-2-deep")
  // assert.Nil(t, err)
  // names = []string{files[0].Name()}
  // assert.Equal(t, []string{"lux.png", names)
}

func TestRequestFstat(t *testing.T) {
  p := clientRequestServerPair(t)
  defer p.Close()
  fp, err := p.cli.Open("/s3tp-test/dir-1-deep/lux.png")
  assert.Nil(t, err)
  fi, err := fp.Stat()
  assert.Nil(t, err)
  assert.Equal(t, fi.Name(), "lux.png")
  assert.Equal(t, fi.Mode(), os.FileMode(0644))
}

func TestRequestRead(t *testing.T) {
  p := clientRequestServerPair(t)
  defer p.Close()

  // get the file
  rf, err := p.cli.Open("/s3tp-test/hello")
  assert.Nil(t, err)
  defer rf.Close()

  // read the contents (from in memory for now)
  contents := make([]byte, 5)
  n, err := rf.Read(contents)
  if err != nil && err != io.EOF {
    t.Fatalf("err: %v", err)
  }

  assert.Equal(t, 5, n)
  assert.Equal(t, "hello", string(contents[0:5]))
}

func TestRequestWrite(t *testing.T) {
  p := clientRequestServerPair(t)
  defer p.Close()
  n, err := putTestFile(p.cli, "s3tp-test/foo", "hello")
  assert.Nil(t, err)
  assert.Equal(t, 5, n)
  r := p.testHandler()
  f, err := r.fetch("/s3tp-test/foo")
  assert.Nil(t, err)
  assert.False(t, f.isdir)
  assert.Equal(t, f.content, []byte("hello"))
}

// func TestRequestMkdir(t *testing.T) {
//   p := clientRequestServerPair(t)
//   defer p.Close()
//   err := p.cli.Mkdir("/foo")
//   assert.Nil(t, err)
//   r := p.testHandler()
//   f, err := r.fetch("/foo")
//   assert.Nil(t, err)
//   assert.True(t, f.isdir)
// }

// setup
func initialize() {
}

var _ = fmt.Print

const sock = "/tmp/rstest.sock"

type csPair struct {
  cli *sftp.Client
  svr *sftp.RequestServer
}

// these must be closed in order, else client.Close will hang
func (cs csPair) Close() {
  cs.svr.Close()
  cs.cli.Close()
  os.Remove(sock)
}

func (cs csPair) testHandler() *s3fs {
  return cs.svr.Handlers.FileGet.(*s3fs)
}

func clientRequestServerPair(t *testing.T) *csPair {
  ready := make(chan bool)
  os.Remove(sock) // either this or signal handling
  var server *sftp.RequestServer
  go func() {
    l, err := net.Listen("unix", sock)
    if err != nil {
      // neither assert nor t.Fatal reliably exit before Accept errors
      panic(err)
    }
    ready <- true
    fd, err := l.Accept()
    assert.Nil(t, err)
    handlers := S3Handler(os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_KEY_ID"))
    server = sftp.NewRequestServer(fd, handlers)
    server.Serve()
  }()
  <-ready
  defer os.Remove(sock)
  c, err := net.Dial("unix", sock)
  assert.Nil(t, err)
  client, err := sftp.NewClientPipe(c, c)
  if err != nil {
    t.Fatalf("%+v\n", err)
  }
  return &csPair{client, server}
}

// tests from example
func putTestFile(cli *sftp.Client, path, content string) (int, error) {
  w, err := cli.Create(path)
  if err == nil {
    defer w.Close()
    return w.Write([]byte(content))
  }
  return 0, err
}

// // needs fail check
// func TestRequestFilename(t *testing.T) {
//   p := clientRequestServerPair(t)
//   defer p.Close()
//   _, err := putTestFile(p.cli, "/foo", "hello")
//   assert.Nil(t, err)
//   r := p.testHandler()
//   f, err := r.fetch("/foo")
//   assert.Nil(t, err)
//   assert.Equal(t, f.Name(), "foo")
// }


// func TestRequestReadFail(t *testing.T) {
//   p := clientRequestServerPair(t)
//   defer p.Close()
//   rf, err := p.cli.Open("/foo")
//   assert.Nil(t, err)
//   contents := make([]byte, 5)
//   n, err := rf.Read(contents)
//   assert.Equal(t, n, 0)
//   assert.Exactly(t, os.ErrNotExist, err)
// }

// func TestRequestOpen(t *testing.T) {
//   p := clientRequestServerPair(t)
//   defer p.Close()
//   fh, err := p.cli.Open("foo")
//   assert.Nil(t, err)
//   err = fh.Close()
//   assert.Nil(t, err)
// }
// func TestRequestRemove(t *testing.T) {
//   p := clientRequestServerPair(t)
//   defer p.Close()
//   _, err := putTestFile(p.cli, "/foo", "hello")
//   assert.Nil(t, err)
//   r := p.testHandler()
//   _, err = r.fetch("/foo")
//   assert.Nil(t, err)
//   err = p.cli.Remove("/foo")
//   assert.Nil(t, err)
//   _, err = r.fetch("/foo")
//   assert.Equal(t, err, os.ErrNotExist)
// }

// func TestRequestRename(t *testing.T) {
//   p := clientRequestServerPair(t)
//   defer p.Close()
//   _, err := putTestFile(p.cli, "/foo", "hello")
//   assert.Nil(t, err)
//   r := p.testHandler()
//   _, err = r.fetch("/foo")
//   assert.Nil(t, err)
//   err = p.cli.Rename("/foo", "/bar")
//   assert.Nil(t, err)
//   _, err = r.fetch("/bar")
//   assert.Nil(t, err)
//   _, err = r.fetch("/foo")
//   assert.Equal(t, err, os.ErrNotExist)
// }

// func TestRequestRenameFail(t *testing.T) {
//   p := clientRequestServerPair(t)
//   defer p.Close()
//   _, err := putTestFile(p.cli, "/foo", "hello")
//   assert.Nil(t, err)
//   _, err = putTestFile(p.cli, "/bar", "goodbye")
//   assert.Nil(t, err)
//   err = p.cli.Rename("/foo", "/bar")
//   assert.IsType(t, &StatusError{}, err)
// }

// func TestRequestStat(t *testing.T) {
//   p := clientRequestServerPair(t)
//   defer p.Close()
//   _, err := putTestFile(p.cli, "/foo", "hello")
//   assert.Nil(t, err)
//   fi, err := p.cli.Stat("/foo")
//   assert.Equal(t, fi.Name(), "foo")
//   assert.Equal(t, fi.Size(), int64(5))
//   assert.Equal(t, fi.Mode(), os.FileMode(0644))
//   assert.NoError(t, testOsSys(fi.Sys()))
// }

// // NOTE: Setstat is a noop in the request server tests, but we want to test
// // that is does nothing without crapping out.
// func TestRequestSetstat(t *testing.T) {
//   p := clientRequestServerPair(t)
//   defer p.Close()
//   _, err := putTestFile(p.cli, "/foo", "hello")
//   assert.Nil(t, err)
//   mode := os.FileMode(0644)
//   err = p.cli.Chmod("/foo", mode)
//   assert.Nil(t, err)
//   fi, err := p.cli.Stat("/foo")
//   assert.Nil(t, err)
//   assert.Equal(t, fi.Name(), "foo")
//   assert.Equal(t, fi.Size(), int64(5))
//   assert.Equal(t, fi.Mode(), os.FileMode(0644))
//   assert.NoError(t, testOsSys(fi.Sys()))
// }


// func TestRequestStatFail(t *testing.T) {
//   p := clientRequestServerPair(t)
//   defer p.Close()
//   fi, err := p.cli.Stat("/foo")
//   assert.Nil(t, fi)
//   assert.True(t, os.IsNotExist(err))
// }

// func TestRequestSymlink(t *testing.T) {
//   p := clientRequestServerPair(t)
//   defer p.Close()
//   _, err := putTestFile(p.cli, "/foo", "hello")
//   assert.Nil(t, err)
//   err = p.cli.Symlink("/foo", "/bar")
//   assert.Nil(t, err)
//   r := p.testHandler()
//   fi, err := r.fetch("/bar")
//   assert.Nil(t, err)
//   assert.True(t, fi.Mode()&os.ModeSymlink == os.ModeSymlink)
// }

// func TestRequestSymlinkFail(t *testing.T) {
//   p := clientRequestServerPair(t)
//   defer p.Close()
//   err := p.cli.Symlink("/foo", "/bar")
//   assert.True(t, os.IsNotExist(err))
// }

// func TestRequestReadlink(t *testing.T) {
//   p := clientRequestServerPair(t)
//   defer p.Close()
//   _, err := putTestFile(p.cli, "/foo", "hello")
//   assert.Nil(t, err)
//   err = p.cli.Symlink("/foo", "/bar")
//   assert.Nil(t, err)
//   rl, err := p.cli.ReadLink("/bar")
//   assert.Nil(t, err)
//   assert.Equal(t, "foo", rl)
// }

// func TestCleanPath(t *testing.T) {
//   assert.Equal(t, "/", cleanPath("/"))
//   assert.Equal(t, "/", cleanPath("//"))
//   assert.Equal(t, "/a", cleanPath("/a/"))
//   assert.Equal(t, "/a", cleanPath("a/"))
//   assert.Equal(t, "/a/b/c", cleanPath("/a//b//c/"))

//   // filepath.ToSlash does not touch \ as char on unix systems, so os.PathSeparator is used for windows compatible tests
//   bslash := string(os.PathSeparator)
//   assert.Equal(t, "/", cleanPath(bslash))
//   assert.Equal(t, "/", cleanPath(bslash+bslash))
//   assert.Equal(t, "/a", cleanPath(bslash+"a"+bslash))
//   assert.Equal(t, "/a", cleanPath("a"+bslash))
//   assert.Equal(t, "/a/b/c", cleanPath(bslash+"a"+bslash+bslash+"b"+bslash+bslash+"c"+bslash))
// }
