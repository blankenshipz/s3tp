package main

import (
  "errors"
  "io"
  "os"
  "sort"
  "strings"
  _ "log"

  "github.com/aws/aws-sdk-go/aws"
  "github.com/aws/aws-sdk-go/service/s3"
  "github.com/pkg/sftp"
  "github.com/satori/go.uuid"
  _"github.com/rlmcpherson/s3gof3r"
)

var delimiter = "/"

type s3listerat []os.FileInfo

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

func S3Handler(accessKey, secretKey string) sftp.Handlers {
  s3fs := &s3fs{
    S3: s3Client(accessKey, secretKey),
    accessKey: accessKey,
    secretKey: secretKey,
    sessionID: uuid.NewV4(),
  }

  return sftp.Handlers{s3fs, s3fs, s3fs, s3fs}
}

// file-system-y thing that the Hanlders live on
type s3fs struct {
  *s3.S3
  accessKey string
  secretKey string
  sessionID uuid.UUID
}

func (fs *s3fs) file_for_path(p string) (*s3File, error) {
  bucket, key := bucket_parts_from_filepath(p)

  input := &s3.HeadObjectInput{
      Bucket: aws.String(bucket),
      Key:    aws.String(key),
  }

  output, err := fs.HeadObject(input)

  if err != nil {
    if len(fs.files_for_path(p)) > 0 {
      return &s3File{name: p, isdir: true}, nil
    }
    return nil, err
  }

  file := &s3File{
    name: p,
    isdir: false,
    key: key,
    size: *output.ContentLength,
    bucket: bucket,
    s3fs: fs,
  }

  return file, nil
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

    go persist_event(fs.sessionID, fs.accessKey, "LIST", 0)
    return s3listerat(list), nil
  case "Stat":
    file, err := fs.file_for_path(r.Filepath)

    if err != nil {
      return nil, err
    }

    go persist_event(fs.sessionID, fs.accessKey, "STAT", 0)
    return s3listerat([]os.FileInfo{file}), nil
  }
  return nil, nil
}

func (fs *s3fs) Fileread(r *sftp.Request) (io.ReaderAt, error) {
  file, err := fs.file_for_path(r.Filepath)

  if err != nil {
    return nil, err
  }

  err = file.OpenStreamingReader(fs.accessKey, fs.secretKey)

  return io.ReaderAt(file), err
}

func (fs *s3fs) Filecmd(r *sftp.Request) error {
  return errors.New("foobar")
}

func (fs *s3fs) Filewrite(r *sftp.Request) (io.WriterAt, error) {
  bucket, key := bucket_parts_from_filepath(r.Filepath)

  file := &s3File{
    name: r.Filepath,
    isdir: false,
    key: key,
    bucket: bucket,
    s3fs: fs,
  }

  _, err := file.OpenStreamingWriter(fs.accessKey, fs.secretKey)

  return file, err
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

