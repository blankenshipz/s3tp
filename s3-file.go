package main

import (
  "errors"
  "io"
  "math"
  "os"
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

const partsize = 10 * mb

var readConfig = &s3gof3r.Config{
  Concurrency: 1,
  PartSize: partsize,
  NTry: 10,
  Md5Check: false,
  Scheme: "https",
  Client: s3gof3r.ClientWithTimeout(5 * time.Second),
}

type readCounter struct {
  data []byte
  readCount *int64
  length int
  eof bool
}

type orderedS3Reader struct {
  readBuffer map[int64]readCounter
  readPartsCount *int32
  streamingReader io.ReadCloser
  readBufferLock sync.RWMutex
}

type orderedS3Writer struct {
  nextOffset int64
  writeBuffer map[int64][]byte
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

func (f *s3File) ReadAt(buffer []byte, offset int64) (int, error) {
  // check my map for the data
  f.readBufferLock.Lock()

  if f.readBuffer == nil {
    f.readBuffer = make(map[int64]readCounter)
    f.readPartsCount = new(int32)
  }

  partNumber := int64(math.Floor(float64(offset) / float64(readConfig.PartSize)))
  // ensure we've read up to this point
  // we might be able to switch the map concept for a simple array
  // the keys are consecutive integers
  for i := int64(*f.readPartsCount); i <= partNumber; i++ {
    buf := make([]byte, readConfig.PartSize)
    n, err := f.streamingReader.Read(buf)
    if err != nil && err != io.EOF {
      return 0, err
    }
    *f.readPartsCount = *f.readPartsCount + 1

    if err == io.EOF {
      f.readBuffer[i] = readCounter{data: buf, readCount: new(int64), length: n, eof: true}
    } else {
      f.readBuffer[i] = readCounter{data: buf, readCount: new(int64), length: n, eof: false}
    }
  }

  val, ok := f.readBuffer[partNumber]

  if !ok {
    return 0, errors.New("cannot read from s3")
  }

  lengthNeeded := int64(len(buffer))
  dataLength := int64(val.length)

  start := (offset - (readConfig.PartSize * partNumber))
  end := start + lengthNeeded

  // if the end needed surpasses this part read what we can and then
  // read additional data from the following part(s) (recurse)
  if end > dataLength && !val.eof {
    copy(buffer, val.data[start:])

    bytesRead := dataLength - start

    *val.readCount += bytesRead

    readMoreSize := lengthNeeded - bytesRead
    readMoreBuffer := make([]byte, readMoreSize)
    readMoreOffset := dataLength + 1

    f.readBufferLock.Unlock()

    _, err := f.ReadAt(readMoreBuffer, readMoreOffset)

    if err != nil {
      return 0, err
    }

    copy(buffer[bytesRead + 1:], readMoreBuffer)
  } else { // we can get everything we need right here
    if end <= dataLength {
      copy(buffer, val.data[start:end])
      *val.readCount += lengthNeeded
    } else { // EOF
      copy(buffer, val.data[start:dataLength])
      *val.readCount += (dataLength - start)
    }

    f.readBufferLock.Unlock()
  }
  // if all the data has been read remove this part
  // we may want to add some stuff here around reorganizing the map
  if *val.readCount == dataLength {
    delete(f.readBuffer, partNumber)
  }

  if end >= dataLength && val.eof {
    return int((dataLength - start)), io.EOF
  } else {
    return len(buffer), nil
  }
}

func (f *s3File) WriteAt(data []byte, offset int64) (int, error) {
  f.writeBufferLock.Lock()

  if f.writeBuffer == nil { // somebody's first time?
    f.writeBuffer = make(map[int64][]byte)
  }

  if offset == f.nextOffset { // we have a hit!
    f.streamingWriter.Write(data)

    delete(f.writeBuffer, offset) // if we recursed to get here this could happen

    f.nextOffset = offset + int64(len(data))
  } else {                    // we have a miss!
    f.writeBuffer[offset] = data
  }

  // If we hit maybe we wrote some data that got us up to an offset that we
  // have in our map already. Write that piece and recurse
  if value, ok := f.writeBuffer[f.nextOffset]; ok {
    // someone else's problem
    f.writeBufferLock.Unlock()
    // Recurse on our data structure
    f.WriteAt(value, f.nextOffset)

  } else {
    f.writeBufferLock.Unlock()
  }

  return len(data), nil
}

func (f *s3File) OpenStreamingReader(accessKey, secretKey string) (error) {
  if f.streamingReader != nil {
    return nil
  }

  keys := s3gof3r.Keys{AccessKey: accessKey, SecretKey: secretKey}

  s3 := s3gof3r.New("", keys)
  b := s3.Bucket(f.bucket)

  r, _, err := b.GetReader(f.key, readConfig)

  if err != nil {
    return err
  }

  f.streamingReader = r

  return nil
}

func (f *s3File) OpenStreamingWriter(accessKey, secretKey string) (io.WriteCloser, error) {
  if f.streamingWriter != nil {
    return f.streamingWriter, nil
  }

  keys := s3gof3r.Keys{AccessKey: accessKey, SecretKey: secretKey}

  s3 := s3gof3r.New("", keys)
  b := s3.Bucket(f.bucket)

  w, err := b.PutWriter(f.key, nil, nil)

  if err != nil {
    return nil, err
  }

  f.streamingWriter = w

  return w, nil
}

func (f *s3File) Close() (error) {
  if f.streamingWriter != nil {
    f.streamingWriter.Close()
  }

  if f.streamingReader != nil {
    f.streamingReader.Close()
  }

  return nil
}
