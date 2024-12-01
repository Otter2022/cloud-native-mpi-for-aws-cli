package aws

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Client struct {
	Client *s3.Client
	Bucket string
}

// NewS3Client initializes a new S3 client
func NewS3Client(bucket string) (*S3Client, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-west-2"))
	if err != nil {
		return nil, fmt.Errorf("unable to load SDK config, %v", err)
	}
	client := s3.NewFromConfig(cfg)
	return &S3Client{Client: client, Bucket: bucket}, nil
}

// UploadFile uploads a local file to the specified S3 bucket
func (s *S3Client) UploadFile(localFilePath string, s3Key string) error {
	file, err := os.Open(localFilePath)
	if err != nil {
		return fmt.Errorf("failed to open file %v", err)
	}
	defer file.Close()

	_, err = s.Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(s3Key),
		Body:   file,
	})
	if err != nil {
		return fmt.Errorf("failed to upload file: %v", err)
	}
	log.Printf("Uploaded %s to bucket %s as %s", localFilePath, s.Bucket, s3Key)
	return nil
}

// DownloadFile downloads an S3 object to a local file
func (s *S3Client) DownloadFile(s3Key, downloadPath string) error {
	resp, err := s.Client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(s3Key),
	})
	if err != nil {
		return fmt.Errorf("failed to download file: %v", err)
	}
	defer resp.Body.Close()

	outFile, err := os.Create(downloadPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %v", err)
	}
	defer outFile.Close()

	_, err = outFile.ReadFrom(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read object body: %v", err)
	}
	log.Printf("Downloaded %s to %s", s3Key, downloadPath)
	return nil
}
