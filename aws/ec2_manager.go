// ec2_manager.go
// This file handles the creation, management, and termination of EC2 instances used
// for running the gRPC nodes in the MPI-like framework.
package aws

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// EC2ClientCreator implements the CreateClient interface for EC2
type EC2ClientCreator struct{}

// LaunchEC2Instances launches a specified number of EC2 instances with a given AMI and instance type.
func LaunchEC2Instances(svc *ec2.Client, count int32, ami, keyName string, instanceType types.InstanceType, securityGroupId string, subnetId string) ([]string, error) {
	runResult, err := svc.RunInstances(context.TODO(), &ec2.RunInstancesInput{
		ImageId:      aws.String(ami),  // AMI ID
		InstanceType: instanceType,     // Instance type (e.g., t2.micro)
		MinCount:     aws.Int32(count), // Number of instances
		MaxCount:     aws.Int32(count),
		KeyName:      aws.String(keyName), // The name of your key pair
		NetworkInterfaces: []types.InstanceNetworkInterfaceSpecification{
			{
				AssociatePublicIpAddress: aws.Bool(true),            // Assigns a public IP
				DeviceIndex:              aws.Int32(0),              // Primary network interface
				SubnetId:                 aws.String(subnetId),      // Specifies the subnet
				Groups:                   []string{securityGroupId}, // Security Group
			},
		},
	})
	if err != nil {
		log.Printf("Failed to create EC2 instances: %v", err)
		return nil, err
	}

	instanceIds := []string{}
	for _, instance := range runResult.Instances {
		instanceIds = append(instanceIds, *instance.InstanceId)
	}

	log.Printf("Created instances: %v", instanceIds)
	return instanceIds, nil
}

// DescribeEC2Instances describes running EC2 instances and returns their public IPs
func DescribeEC2Instances(svc *ec2.Client, instanceIds []string) ([]string, error) {
	input := &ec2.DescribeInstancesInput{
		InstanceIds: instanceIds,
	}

	// Call DescribeInstances API
	result, err := svc.DescribeInstances(context.TODO(), input)
	if err != nil {
		return nil, fmt.Errorf("failed to describe EC2 instances: %v", err)
	}

	// Slice to store public IP addresses
	var publicIPs []string

	// Iterate over the instances and collect public IPs
	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			log.Printf("Instance ID: %s", *instance.InstanceId)
			log.Printf("Instance State: %s", instance.State.Name)
			log.Printf("Instance Type: %s", instance.InstanceType)
			if instance.PublicIpAddress != nil {
				publicIP := *instance.PublicIpAddress
				publicIPs = append(publicIPs, publicIP)
				log.Printf("Public IP: %s", publicIP)
			} else {
				log.Println("Public IP: None")
			}
		}
	}

	return publicIPs, nil
}

// TerminateEC2Instances terminates EC2 instances
func TerminateEC2Instances(svc *ec2.Client, instanceIds []string) error {
	input := &ec2.TerminateInstancesInput{
		InstanceIds: instanceIds, // No need to use aws.StringSlice here as it already expects []string
	}

	// Call the TerminateInstances API
	result, err := svc.TerminateInstances(context.TODO(), input)
	if err != nil {
		log.Printf("Failed to terminate EC2 instances: %v", err)
		return err
	}

	for _, instance := range result.TerminatingInstances {
		log.Printf("Terminating instance: %s, current state: %s, previous state: %s",
			*instance.InstanceId,
			instance.CurrentState.Name,
			instance.PreviousState.Name)
	}

	return nil
}

// CreateClient method creates the EC2 client using AWS SDK v2
func (s *EC2ClientCreator) CreateClient() (*ec2.Client, error) {
	var cfg aws.Config
	var err error

	region := os.Getenv("AWS_REGION")
	if region != "" {
		cfg, err = config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	} else {
		cfg, err = config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-east-1"))
	}

	if err != nil {
		return nil, fmt.Errorf("unable to load AWS config: %w", err)
	}

	client := ec2.NewFromConfig(cfg)
	return client, err
}
