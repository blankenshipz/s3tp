package main

import (
  "errors"
  "io"
  "math"
  "os"
  "runtime/debug"
  "strconv"
  "sync"
  "syscall"
  "time"

  "github.com/rlmcpherson/s3gof3r"
)

const(
  _        = iota
  kb int64 = 1 << (10 * iota)
  mb
  gb
  tb
  pb
  eb
)

var concurrency = (func() int {
  val := os.Getenv("CONCURRENCY")

  if val == "" {
    return 1
  } else {
    i, _ := strconv.Atoi(val)

    return i
  }
})()

const partsize = 10 * mb

var gof3rConfig = &s3gof3r.Config{
  Concurrency: concurrency,
  PartSize: partsize,
  NTry: 10,
  Md5Check: false,
  Scheme: "https",
  Client: s3gof3r.ClientWithTimeout(5 * time.Second),
}

type readCounter struct {
  data []byte
  readCount int64
  length int64
  eof bool
}

type orderedS3Reader struct {
  readBuffer *readCounter
  readPartsCount *int64
  readBytesCount int64
  streamingReader io.ReadCloser
  readBufferLock sync.RWMutex
  readWaiters []chan int64
  finishWaiters []chan int64
}

type orderedS3Writer struct {
  nextOffset int64
  writeBuffer map[int64]*[]byte
  writtenBytesCount int64
  streamingWriter io.WriteCloser
  writeBufferLock sync.RWMutex
}

// Implements os.FileInfo, Reader and Writer interfaces.
// These are the 3 interfaces necessary for the Handlers.
type s3File struct {
  name        string
  modtime     time.Time
  symlink     string
  isdir       bool
  content     []byte
  contentLock sync.RWMutex
  bucket      string
  key         string
  size        int64
  orderedS3Writer
  orderedS3Reader
  *s3fs
}

// Have s3File fulfill os.FileInfo interface
func (f *s3File) IsDir() bool {
  return f.isdir
}

func (f *s3File) Mode() os.FileMode {
  ret := os.FileMode(0644)

  if f.isdir {
    ret = os.FileMode(0755) | os.ModeDir
  }
  if f.symlink != "" {
    ret = os.FileMode(0777) | os.ModeSymlink
  }

  return ret
}

func (f *s3File) ModTime() time.Time {
  return time.Now()
}

func (f *s3File) Name() string {
  return f.name
}

func (f *s3File) Size() int64  {
  return f.size
}

func (f *s3File) Sys() interface{} {
  return fakeFileInfoSys()
}

func fakeFileInfoSys() interface{} {
  return &syscall.Stat_t{Uid: 65534, Gid: 65534}
}

func (f *s3File) WriterAt() (io.WriterAt, error) {
  if f.isdir { // This can't happen
    return nil, os.ErrInvalid
  }
  return f, nil
}

func (f *s3File) readNextPart() (int, error) {
  for _, channel := range f.finishWaiters {
    channel <- *f.readPartsCount
  }

  f.readBuffer = new(readCounter)
  buf := make([]byte, gof3rConfig.PartSize)

  n, err := f.streamingReader.Read(buf)

  if err != nil && err != io.EOF {
    return 0, err
  }

  *f.readPartsCount = *f.readPartsCount + 1
  f.readBuffer.length = int64(n)
  f.readBuffer.data = buf

  if err == io.EOF {
    f.readBuffer.eof = true
  } else {
    f.readBuffer.eof = false
  }

  for _, channel := range f.readWaiters {
    channel <- *f.readPartsCount
  }

  return n, nil
}

func (f *s3File) waitFor(partNumber int64, waiters *[]chan int64) {
    waiting := make(chan int64)
    *waiters = append(*waiters, waiting)

    f.readBufferLock.Unlock()
    for i := range waiting {
      if i == partNumber {
        break
      }
    }
    f.readBufferLock.Lock()

    // remove this waiter
    for i, x := range *waiters {
      if x == waiting {
        (*waiters)[i] = (*waiters)[len(*waiters)-1]
        (*waiters)[len(*waiters)-1] = nil
        *waiters = (*waiters)[:len(*waiters)-1]
      }
    }
    close(waiting)
}

func (f *s3File) ReadAt(buffer []byte, offset int64) (int, error) {
  f.readBufferLock.Lock()
  defer f.readBufferLock.Unlock()

  if f.readBuffer == nil {
    f.readPartsCount = new(int64)
    *f.readPartsCount = -1
    f.readNextPart()
  }

  partNumber := int64(math.Floor(float64(offset) / float64(gof3rConfig.PartSize)))

  // If we haven't gottten to a part that contains this data then wait for it
  if partNumber != *f.readPartsCount {
    if partNumber < *f.readPartsCount {
      return 0, errors.New("Offset not available for reading")
    }

    f.waitFor(partNumber, &f.readWaiters)
  }

  lengthNeeded := int64(len(buffer))
  dataLength := int64(f.readBuffer.length)
  start := (offset - (gof3rConfig.PartSize * partNumber))
  end := start + lengthNeeded

  // if we need more then we have then stuff the buffer with what we've got and
  // ask for more - however we need to make sure this buffer is done before we
  // move on
  readBytesCount := int64(0)
  if end > dataLength && !f.readBuffer.eof {
    copy(buffer, f.readBuffer.data[start:])
    bytesRead := dataLength - start
    f.readBuffer.readCount += bytesRead

    if f.readBuffer.readCount != f.readBuffer.length {
      f.waitFor(partNumber, &f.finishWaiters)
    } else {
      f.readNextPart()
    }

    readMoreSize := lengthNeeded - bytesRead
    readMoreBuffer := make([]byte, readMoreSize)
    readMoreOffset := dataLength + 1

    f.readBufferLock.Unlock()

    // err could be EOF
    _, err := f.ReadAt(readMoreBuffer, readMoreOffset)

    f.readBufferLock.Lock()

    if err != nil && err != io.EOF {
      return 0, err
    }

    copy(buffer[bytesRead + 1:], readMoreBuffer)
    readBytesCount = lengthNeeded
  } else { // we can get everything we need right here
    if end <= dataLength {
      copy(buffer, f.readBuffer.data[start:end])
      f.readBuffer.readCount += lengthNeeded
      readBytesCount = lengthNeeded
    } else { // EOF
      if start <= dataLength {
        copy(buffer, f.readBuffer.data[start:dataLength])
        readBytesCount = (dataLength - start)
        f.readBuffer.readCount += readBytesCount
      }
    }
  }

  f.readBytesCount += readBytesCount

  if f.readBuffer.readCount == f.readBuffer.length {
    f.readNextPart()
  }
  // if all the data has been read remove this part
  // we may want to add some stuff here around reorganizing the map
  if f.readBuffer.eof {
    return int(readBytesCount), io.EOF
  } else {
    return int(readBytesCount), nil
  }
}

func (f *s3File) WriteAt(data []byte, offset int64) (int, error) {
  f.writeBufferLock.Lock()

  if f.writeBuffer == nil { // somebody's first time?
    f.writeBuffer = make(map[int64]*[]byte)
  }

  if offset == f.nextOffset { // we have a hit!
    f.writtenBytesCount += int64(len(data))
    f.streamingWriter.Write(data)

    delete(f.writeBuffer, offset) // if we recursed to get here this could happen

    f.nextOffset = offset + int64(len(data))
  } else {                    // we have a miss!
    f.writeBuffer[offset] = &data
  }

  // If we hit maybe we wrote some data that got us up to an offset that we
  // have in our map already. Write that piece and recurse
  if value, ok := f.writeBuffer[f.nextOffset]; ok {
    // someone else's problem
    f.writeBufferLock.Unlock()
    // Recurse on our data structure
    f.WriteAt(*value, f.nextOffset)

  } else {
    f.writeBufferLock.Unlock()
  }

  return len(data), nil
}

func (f *s3File) OpenStreamingReader(accessKey, secretKey string) (error) {
  f.readBufferLock.Lock()
  defer f.readBufferLock.Unlock()

  if f.streamingReader != nil {
    return nil
  }

  keys := s3gof3r.Keys{AccessKey: accessKey, SecretKey: secretKey}

  s3 := s3gof3r.New("", keys)
  b := s3.Bucket(f.bucket)

  r, _, err := b.GetReader(f.key, gof3rConfig)

  if err != nil {
    return err
  }

  f.streamingReader = r

  return nil
}

func (f *s3File) OpenStreamingWriter(accessKey, secretKey string) (io.WriteCloser, error) {
  f.writeBufferLock.Lock()
  defer f.writeBufferLock.Unlock()

  if f.streamingWriter != nil {
    return f.streamingWriter, nil
  }

  keys := s3gof3r.Keys{AccessKey: accessKey, SecretKey: secretKey}

  s3 := s3gof3r.New("", keys)
  b := s3.Bucket(f.bucket)

  w, err := b.PutWriter(f.key, nil, gof3rConfig)

  if err != nil {
    return nil, err
  }

  f.streamingWriter = w

  return w, nil
}

func (f *s3File) Close() (error) {
  var err error

  if f.streamingWriter != nil {
    f.writeBufferLock.Lock()
    defer f.writeBufferLock.Unlock()

    err = f.streamingWriter.Close()
    f.streamingWriter = nil
    debug.FreeOSMemory()

    if f.writtenBytesCount > 0 {
      go persist_event(f.sessionID, f.accessKey, "WRITE", f.writtenBytesCount)
    }
  }

  if f.streamingReader != nil {
    f.readBufferLock.Lock()
    defer f.readBufferLock.Unlock()

    err = f.streamingReader.Close()
    f.streamingReader = nil
    debug.FreeOSMemory()

    if f.readBytesCount > 0 {
      go persist_event(f.sessionID, f.accessKey, "READ", f.readBytesCount)
    }
  }

  if err != nil {
    return err
  }

  return err
}
