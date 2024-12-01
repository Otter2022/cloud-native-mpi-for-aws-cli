// key_pair_manager.go
// This file handles the creation of EC2 key pairs, which allow SSH access to EC2 instances.
// The private key material is returned to the caller for secure access.
package aws

import (
	"context"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

// CreateKeyPair creates a new key pair in AWS EC2
func CreateKeyPair(svc *ec2.Client, keyName string) {
	input := &ec2.CreateKeyPairInput{
		KeyName: aws.String(keyName),
	}

	// v2 call includes the context.Context as the first argument
	result, err := svc.CreateKeyPair(context.TODO(), input)
	if err != nil {
		log.Printf("Failed to create key pair: %v", err)
	}

	log.Printf("Created key pair: %s", *result.KeyName)
	log.Printf("Private Key Material: \n%s", *result.KeyMaterial)

	// You may want to save the private key to a file for later use
}

// Delete a key pair
func DeleteKeyPair(svc *ec2.Client, keyName string) error {
	input := &ec2.DeleteKeyPairInput{
		KeyName: aws.String(keyName),
	}

	// Call DeleteKeyPair API
	_, err := svc.DeleteKeyPair(context.TODO(), input)
	if err != nil {
		log.Printf("Failed to delete key pair: %v", err)
		return err
	}

	log.Printf("Successfully deleted key pair: %s", keyName)
	return nil
}

// Describe a key pair
func DescribeKeyPair(svc *ec2.Client, keyName string) {
	var input *ec2.DescribeKeyPairsInput

	if keyName != "" {
		// If keyName is provided, describe the specific key pair
		input = &ec2.DescribeKeyPairsInput{
			KeyNames: []string{keyName},
		}
	} else {
		// Describe all key pairs if no specific key name is given
		input = &ec2.DescribeKeyPairsInput{}
	}

	// Call DescribeKeyPairs API
	result, err := svc.DescribeKeyPairs(context.TODO(), input)
	if err != nil {
		log.Printf("Failed to describe key pairs: %v", err)
		return
	}

	for _, keyPair := range result.KeyPairs {
		log.Printf("Key Pair Name: %s, Fingerprint: %s", *keyPair.KeyName, *keyPair.KeyFingerprint)
	}
}
