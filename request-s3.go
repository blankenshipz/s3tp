package main

import (
  "bytes"
  "errors"
  "io"
  _ "log"
  "os"
  "sort"
  "strings"
  "sync"
  "syscall"
  "time"

  "github.com/aws/aws-sdk-go/aws"
  "github.com/aws/aws-sdk-go/aws/credentials"
  "github.com/aws/aws-sdk-go/aws/session"
  "github.com/aws/aws-sdk-go/service/s3"
  "github.com/pkg/sftp"
  _"github.com/rlmcpherson/s3gof3r"
)

type s3listerat []os.FileInfo
var delimiter = "/"

// In memory file-system-y thing that the Hanlders live on
type s3fs struct {
  *s3.S3
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
}

func s3Client(access_key_id, secret_access_key string) (*s3.S3) {
  value := credentials.Value{ AccessKeyID: access_key_id, SecretAccessKey: secret_access_key}
  creds := credentials.NewStaticCredentialsFromCreds(value)
  sess, _ := session.NewSession(&aws.Config{ Region: aws.String("us-east-1") })
  client := s3.New(sess, &aws.Config{Credentials: creds})
  return client
}

func bucket_parts_from_filepath(p string) (bucket, path string) {
    list := strings.Split(p, delimiter)
    bucket = strings.TrimSpace(list[1])

    path = ""

    if len(list) > 2 {
      path = strings.Join(list[2:len(list)], delimiter)
    }

    return bucket, path
}

func (fs *s3fs) file_for_path(p string) (*s3File, error) {
  bucket, key := bucket_parts_from_filepath(p)

  input := &s3.HeadObjectInput{
      Bucket: aws.String(bucket),
      Key:    aws.String(key),
  }

  _, err := fs.HeadObject(input)

  if err != nil {
    if len(fs.files_for_path(p)) > 0 {
      return &s3File{name: p, isdir: true}, nil
    }
    return nil, err
  }
  return &s3File{name: p, isdir: false, key: p}, nil
}

func (fs *s3fs) files_for_path(p string) (map[string]*s3File) {
  files := make(map[string]*s3File)

  if p == "/" {
    bucket_results, _ := fs.ListBuckets(&s3.ListBucketsInput{})

    for _, bucket := range bucket_results.Buckets {
      files[*bucket.Name] = &s3File{name: *bucket.Name, isdir: true}
    }
  } else {
    bucket, prefix := bucket_parts_from_filepath(p)

    if prefix != "" {
      prefix = prefix + "/"
    }

    input := &s3.ListObjectsV2Input{
        Bucket:  aws.String(bucket),
        MaxKeys: aws.Int64(1000),
        Delimiter: &delimiter,
        Prefix: &prefix,
    }

    result, _ := fs.ListObjectsV2(input)

    for _, f  := range result.Contents {
      if *f.Key == prefix {
        continue
      }

      name := strings.TrimPrefix(*f.Key, prefix)
      files[*f.Key] = &s3File{name: name, bucket: bucket, key: *f.Key}
    }

    for _, f  := range result.CommonPrefixes {
      path := strings.TrimPrefix(*f.Prefix, prefix)
      dir := strings.TrimSuffix(path, delimiter)
      files[*f.Prefix] = &s3File{name: dir, bucket: bucket, isdir: true}
    }
  }

  return files
}

// Modeled after strings.Reader's ReadAt() implementation
func (f s3listerat) ListAt(ls []os.FileInfo, offset int64) (int, error) {
  var n int
  if offset >= int64(len(f)) {
    return 0, io.EOF
  }
  n = copy(ls, f[offset:])
  if n < len(ls) {
    return n, io.EOF
  }
  return n, nil
}

func S3Handler(access_key, secret_key string) sftp.Handlers {
  s3fs := &s3fs{S3: s3Client(access_key, secret_key)}
  return sftp.Handlers{s3fs, s3fs, s3fs, s3fs}
}

func (fs *s3fs) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
  switch r.Method {
  case "List":
    ordered_names := []string{}
    files := fs.files_for_path(r.Filepath)

    for fn, _ := range files { ordered_names = append(ordered_names, fn) }

    sort.Sort(sort.StringSlice(ordered_names))

    list := make([]os.FileInfo, len(ordered_names))

    for i, fn := range ordered_names {
      list[i] = files[fn]
    }

    return s3listerat(list), nil
  case "Stat":
    file, err := fs.file_for_path(r.Filepath)

    if err != nil {
      return nil, err
    }
    return s3listerat([]os.FileInfo{file}), nil
  }
  return nil, nil
}

func (fs *s3fs) fetch(filepath string) (*s3File, error) {
  if filepath == "/" {
    return nil, nil
  }

  // TODO could probably remove the overhead here
  file, err := fs.file_for_path(filepath)
  buffer := new(bytes.Buffer)

  if err != nil {
    return nil, err
  }

  input := &s3.GetObjectInput{
      Bucket: aws.String(file.bucket),
      Key:    aws.String(file.key),
  }

  result, err := fs.GetObject(input)

  buffer.ReadFrom(result.Body)

  file.content = buffer.Bytes()

  return file, nil
}

func (fs *s3fs) Fileread(r *sftp.Request) (io.ReaderAt, error) {
  file, err := fs.fetch(r.Filepath)

  if err != nil {
    return nil, err
  }
  if file.symlink != "" {
    file, err = fs.fetch(file.symlink)
    if err != nil {
      return nil, err
    }
  }

  return file.ReaderAt()
}

func (fs *s3fs) Filecmd(r *sftp.Request) error {
  return errors.New("foobar")
}

func (fs *s3fs) Filewrite(r *sftp.Request) (io.WriterAt, error) {
  return nil, errors.New("foobar")
}

func (f *s3File) WriterAt() (io.WriterAt, error) {
  if f.isdir {
    return nil, os.ErrInvalid
  }
  return nil, nil
}

func (f *s3File) ReaderAt() (io.ReaderAt, error) {
  if f.isdir {
    return nil, os.ErrInvalid
  }

  return bytes.NewReader(f.content), nil
}

func fakeFileInfoSys() interface{} {
  return &syscall.Stat_t{Uid: 65534, Gid: 65534}
}

// Have s3File fulfill os.FileInfo interface
func (f *s3File) Name() string { return f.name }
func (f *s3File) Size() int64  { return 100 }

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

func (f *s3File) ModTime() time.Time { return time.Now() }
func (f *s3File) IsDir() bool        { return f.isdir }
func (f *s3File) Sys() interface{} { return fakeFileInfoSys() }
