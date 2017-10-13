package main

import (
  "github.com/aws/aws-sdk-go/aws"
  "github.com/aws/aws-sdk-go/aws/credentials"
  "github.com/aws/aws-sdk-go/aws/session"
  "github.com/aws/aws-sdk-go/service/s3"
)

func s3Client(access_key_id, secret_access_key string) (*s3.S3) {
  value := credentials.Value{ AccessKeyID: access_key_id, SecretAccessKey: secret_access_key}
  creds := credentials.NewStaticCredentialsFromCreds(value)
  sess, _ := session.NewSession(&aws.Config{ Region: aws.String("us-east-1") })
  client := s3.New(sess, &aws.Config{Credentials: creds})

  return client
}

