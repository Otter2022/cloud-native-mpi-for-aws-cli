// cmd/root.go

package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	awsManager "github.com/Otter2022/cloud-native-mpi-for-aws-cli/aws"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmTypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/spf13/cobra"
)

var (
	numInstances   int
	vpcID          string
	executablePath string
)

var rootCmd = &cobra.Command{
	Use:   "awsmpirun",
	Short: "Distribute and run MPI-like programs on AWS EC2 instances",
	Long: `awsmpirun is a CLI tool that runs a program across existing EC2 instances
in a VPC, assigns ranks, and sets up environment variables for MPI-like communication.`,
	Run: func(cmd *cobra.Command, args []string) {
		runAWSMPIRun()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	// Define flags
	rootCmd.Flags().IntVarP(&numInstances, "num-instances", "n", 1, "Number of EC2 instances")
	rootCmd.Flags().StringVarP(&vpcID, "vpc", "v", "", "VPC ID (required)")
	rootCmd.Flags().StringVarP(&executablePath, "exec", "e", "", "Path to the executable on the instances (required)")

	// Mark required flags
	rootCmd.MarkFlagRequired("vpc")
	rootCmd.MarkFlagRequired("exec")
}

func runAWSMPIRun() {
	// Step 1: Discover EC2 instances in the VPC
	instances, err := discoverInstances(vpcID)
	if err != nil {
		fmt.Printf("Error discovering instances: %v\n", err)
		os.Exit(1)
	}

	// Step 2: Select the required number of instances
	if len(instances) < numInstances {
		fmt.Printf("Not enough instances in the VPC. Requested: %d, Available: %d\n", numInstances, len(instances))
		os.Exit(1)
	}
	selectedInstances := instances[:numInstances]

	// Step 3: Assign ranks
	assignRanks(selectedInstances)

	// Step 4: Execute the program on all instances
	err = executeProgram(selectedInstances)
	if err != nil {
		fmt.Printf("Error executing program: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Program executed successfully on all instances.")
}

func discoverInstances(vpcID string) ([]awsManager.InstanceInfo, error) {
	// Initialize EC2 client
	ec2ClientCreator := awsManager.EC2ClientCreator{}
	ec2Client, err := ec2ClientCreator.CreateClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create EC2 client: %v", err)
	}

	// Describe instances with filters
	input := &ec2.DescribeInstancesInput{
		Filters: []ec2Types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{vpcID},
			},
			{
				Name:   aws.String("instance-state-name"),
				Values: []string{"running"},
			},
		},
	}

	result, err := ec2Client.DescribeInstances(context.TODO(), input)
	if err != nil {
		return nil, fmt.Errorf("failed to describe instances: %v", err)
	}

	var instances []awsManager.InstanceInfo
	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			if instance.InstanceId != nil && instance.PrivateIpAddress != nil {
				instances = append(instances, awsManager.InstanceInfo{
					InstanceID:   *instance.InstanceId,
					PrivateIP:    *instance.PrivateIpAddress,
					PublicIP:     aws.ToString(instance.PublicIpAddress),
					InstanceRank: -1, // Initialize with -1
				})
			}
		}
	}

	return instances, nil
}

func assignRanks(instances []awsManager.InstanceInfo) {
	// Assign ranks to instances
	for i := range instances {
		instances[i].InstanceRank = i
	}
}

func executeProgram(instances []awsManager.InstanceInfo) error {
	ssmClientCreator := awsManager.SSMClientCreator{}
	ssmClient, err := ssmClientCreator.CreateClient()
	if err != nil {
		return fmt.Errorf("failed to create SSM client: %v", err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	errorsOccurred := false

	// Map to store command IDs for each instance
	commandIDs := make(map[string]string)

	for _, instance := range instances {
		wg.Add(1)
		go func(instance awsManager.InstanceInfo) {
			defer wg.Done()

			// Prepare environment variables as a string
			var envVars []string
			envVars = append(envVars, fmt.Sprintf("export MPI_RANK=%d", instance.InstanceRank))
			envVars = append(envVars, fmt.Sprintf("export MPI_SIZE=%d", len(instances)))

			for _, inst := range instances {
				address := inst.PrivateIP
				if address == "" {
					address = inst.PublicIP
				}
				if inst.InstanceID == instance.InstanceID {
					address = "0.0.0.0" // For the local instance
				}
				envVars = append(envVars, fmt.Sprintf(`export MPI_ADDRESS_%d="%s:50051"`, inst.InstanceRank, address))
			}

			// Build the script to set environment variables and run the program
			script := fmt.Sprintf(`#!/bin/bash
%s
%s > output.txt 2>&1
`, strings.Join(envVars, "\n"), executablePath)

			input := &ssm.SendCommandInput{
				DocumentName: aws.String("AWS-RunShellScript"),
				Parameters: map[string][]string{
					"commands": {script},
				},
				InstanceIds:    []string{instance.InstanceID},
				TimeoutSeconds: aws.Int32(600),
			}
			result, err := ssmClient.SendCommand(context.TODO(), input)
			if err != nil {
				fmt.Printf("Failed to execute program on instance %s: %v\n", instance.InstanceID, err)
				mu.Lock()
				errorsOccurred = true
				mu.Unlock()
				return
			}

			// Store the command ID for later retrieval
			commandID := *result.Command.CommandId
			mu.Lock()
			commandIDs[instance.InstanceID] = commandID
			mu.Unlock()
		}(instance)
	}

	wg.Wait()

	if errorsOccurred {
		return fmt.Errorf("errors occurred during program execution")
	}

	// Retrieve and display output from rank 0
	for _, instance := range instances {
		if instance.InstanceRank == 0 {
			commandID := commandIDs[instance.InstanceID]
			output, err := getCommandOutput(ssmClient, commandID, instance.InstanceID)
			if err != nil {
				return fmt.Errorf("failed to get output from rank 0: %v", err)
			}
			fmt.Println("Output from rank 0:")
			fmt.Println(output)
			break
		}
	}

	return nil
}

func getCommandOutput(ssmClient *ssm.Client, commandID, instanceID string) (string, error) {
	input := &ssm.GetCommandInvocationInput{
		CommandId:  aws.String(commandID),
		InstanceId: aws.String(instanceID),
	}

	// Poll for command completion
	for {
		output, err := ssmClient.GetCommandInvocation(context.TODO(), input)
		if err != nil {
			// If it's a throttling error, wait and retry
			if strings.Contains(err.Error(), "ThrottlingException") {
				time.Sleep(2 * time.Second)
				continue
			}
			return "", fmt.Errorf("failed to get command invocation: %v", err)
		}

		status := output.Status
		if status == ssmTypes.CommandInvocationStatusInProgress || status == ssmTypes.CommandInvocationStatusPending {
			time.Sleep(2 * time.Second)
			continue
		}

		if status == ssmTypes.CommandInvocationStatusSuccess {
			return aws.ToString(output.StandardOutputContent), nil
		}

		return "", fmt.Errorf("command failed with status %s: %s", status, aws.ToString(output.StandardErrorContent))
	}
}
