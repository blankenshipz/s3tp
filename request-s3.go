package main

// This serves as an example of how to implement the request server handler as
// well as a dummy backend for testing. It implements an in-memory backend that
// works as a very simple filesystem with simple flat key-value lookup system.

import (
  "errors"
  "io"
  _"log"
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
)

type s3listerat []os.FileInfo
var delimiter = "/"

// In memory file-system-y thing that the Hanlders live on
type s3fs struct {
  *s3File
}

func s3Client() (*s3.S3) {
  value := credentials.Value{ AccessKeyID: os.Getenv("AWS_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY") }
  creds := credentials.NewStaticCredentialsFromCreds(value)
  sess, _ := session.NewSession(&aws.Config{ Region: aws.String("us-east-1") })
  client := s3.New(sess, &aws.Config{Credentials: creds})
  return client
}

/*
  * We should have the request pathname, if possible use that to pick a single bucket
  * Otherwise get the list of all buckets
  * Poll for the bucket(s) region and create a client for each region that has buckets
  * get the list of files in the bucket(s)
  * create virtual set of files and "IsDir" for the files from the ObjectList

  * If possible cache the list of buckets and their regions between requests
*/
func (fs *s3fs) files_for_path(p string) (map[string]*s3File) {
  files := make(map[string]*s3File)
  client := s3Client()

  if p == "/" {
    bucket_results, _ := client.ListBuckets(&s3.ListBucketsInput{})

    for _, bucket := range bucket_results.Buckets {
      files[*bucket.Name] = &s3File{name: *bucket.Name, isdir: true}
    }
  } else {
    list := strings.Split(p, delimiter)
    bucket := strings.TrimSpace(list[1])
    prefix := ""

    if len(list) > 2 {
      prefix = strings.Join(list[2:len(list)], delimiter)
      prefix = prefix + "/"
    }

    input := &s3.ListObjectsV2Input{
        Bucket:  aws.String(bucket),
        MaxKeys: aws.Int64(1000),
        Delimiter: &delimiter,
        Prefix: &prefix,
    }

    result, _ := client.ListObjectsV2(input)

    for _, f  := range result.Contents {
      if *f.Key == prefix {
        continue
      }

      name := strings.TrimPrefix(*f.Key, prefix)
      files[*f.Key] = &s3File{name: name, bucket: bucket}
    }

    for _, f  := range result.CommonPrefixes {
      name := strings.TrimPrefix(*f.Prefix, prefix)
      files[*f.Prefix] = &s3File{name: name, bucket: bucket, isdir: true}
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

func S3Handler() sftp.Handlers {
  s3fs := &s3fs{}
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
  }
  return nil, nil
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
}

func (fs *s3fs) fetch(path string) (*s3File, error) {
  if path == "/" {
    return nil, nil
  }
  return nil, os.ErrNotExist
}

// factory to make sure modtime is set
func newS3File(name string, isdir bool, bucket string) *s3File {
  return &s3File{
    name:    name,
    modtime: time.Now(),
    isdir:   isdir,
    bucket:  bucket,
  }
}

func (fs *s3fs) Fileread(r *sftp.Request) (io.ReaderAt, error) {
  return nil, errors.New("foobar")
}

func (fs *s3fs) Filecmd(r *sftp.Request) error {
  return errors.New("foobar")
}

func (fs *s3fs) Filewrite(r *sftp.Request) (io.WriterAt, error) {
  return nil, errors.New("foobar")
}

func fakeFileInfoSys() interface{} {
  return &syscall.Stat_t{Uid: 65534, Gid: 65534}
}

// Have s3File fulfill os.FileInfo interface
func (f *s3File) Name() string { return f.name }
func (f *s3File) Size() int64  { return 100 }
func (f *s3File) Mode() os.FileMode { return os.FileMode(0644) }
func (f *s3File) ModTime() time.Time { return time.Now() }
func (f *s3File) IsDir() bool        { return f.isdir }
func (f *s3File) Sys() interface{} { return fakeFileInfoSys() }
