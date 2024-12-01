// deployment_manager.go
// deployment_manager.go
package aws

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// InstanceInfo holds the instance ID and IP address
type InstanceInfo struct {
	InstanceID   string
	PrivateIP    string
	InstanceRank int
}

type SSMClientCreator struct{}

// GetInstanceIPs fetches the instance IDs and IP addresses of all instances in the specified subnet
func GetInstanceIPandIDs(client *ec2.Client, subnetID string) ([]InstanceInfo, error) {
	input := &ec2.DescribeInstancesInput{
		Filters: []ec2Types.Filter{
			{
				Name:   aws.String("subnet-id"),
				Values: []string{subnetID},
			},
		},
	}

	var instances []InstanceInfo
	paginator := ec2.NewDescribeInstancesPaginator(client, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.TODO())
		if err != nil {
			return nil, err
		}
		for _, reservation := range page.Reservations {
			for _, instance := range reservation.Instances {
				if instance.InstanceId != nil && instance.PrivateIpAddress != nil {
					instances = append(instances, InstanceInfo{
						InstanceID: *instance.InstanceId,
						PrivateIP:  *instance.PrivateIpAddress,
					})
				}
			}
		}
	}
	return instances, nil
}

func InitializeEnviromentsAndBuild(client *ssm.Client, instances []InstanceInfo) ([]InstanceInfo, error) {
	n := len(instances)
	var wg sync.WaitGroup
	var mu sync.Mutex
	errorsOccurred := false

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var envVars []string
			envVars = append(envVars, fmt.Sprintf("export MPI_SIZE=%d", n))
			envVars = append(envVars, fmt.Sprintf("export MPI_RANK=%d", i))

			for x := 0; x < n; x++ {
				if x == i {
					envVars = append(envVars, fmt.Sprintf("export MPI_ADDRESS_%d=\"0.0.0.0:50051\"", x))
					instances[i].InstanceRank = i
				} else {
					envVars = append(envVars, fmt.Sprintf("export MPI_ADDRESS_%d=\"%s:50051\"", x, instances[x].PrivateIP))
				}
			}

			// Combine commands into a single script
			script := `#!/bin/bash
%s
cd cloud-native-mpi-for-aws
./mpi_program > output.txt`

			allCommands := strings.Join(envVars, "\n")
			finalScript := fmt.Sprintf(script, allCommands)

			fmt.Printf("%v", finalScript)

			input := &ssm.SendCommandInput{
				DocumentName: aws.String("AWS-RunShellScript"),
				Parameters: map[string][]string{
					"commands": {finalScript},
				},
				InstanceIds:    []string{instances[i].InstanceID},
				TimeoutSeconds: aws.Int32(600),
			}
			result, err := client.SendCommand(context.TODO(), input)
			if err != nil {
				fmt.Printf("Failed to send command to instance %s: %v\n", instances[i].InstanceID, err)
				mu.Lock()
				errorsOccurred = true
				mu.Unlock()
				return
			} else {
				fmt.Printf("SSM Command Result for instance %s: %v\n", instances[i].InstanceID, result)
			}

			// Optionally, wait for command execution to complete and collect outputs
			// This can be done using GetCommandInvocation

			// Get Command Invocation Result
			commandID := *result.Command.CommandId
			invocationInput := &ssm.GetCommandInvocationInput{
				CommandId:  aws.String(commandID),
				InstanceId: aws.String(instances[i].InstanceID),
			}

			// Poll for command completion
			for {
				invocationResult, err := client.GetCommandInvocation(context.TODO(), invocationInput)
				if err != nil {
					fmt.Printf("Failed to get command invocation for instance %s: %v\n", instances[i].InstanceID, err)
					mu.Lock()
					errorsOccurred = true
					mu.Unlock()
					return
				}

				// Check status by comparing to string values
				if invocationResult.Status != "InProgress" && invocationResult.Status != "Pending" {
					fmt.Printf("Command Invocation Status for instance %s: %s\n", instances[i].InstanceID, invocationResult.Status)
					fmt.Printf("Standard Output for instance %s:\n%s\n", instances[i].InstanceID, aws.ToString(invocationResult.StandardOutputContent))
					fmt.Printf("Standard Error for instance %s:\n%s\n", instances[i].InstanceID, aws.ToString(invocationResult.StandardErrorContent))
					break
				}

				// Sleep for a short duration before polling again
				time.Sleep(2 * time.Second)
			}
		}(i)
	}

	wg.Wait()

	if errorsOccurred {
		return instances, fmt.Errorf("errors occurred during command execution")
	}

	return instances, nil
}

// CreateClient method creates the EC2 client using AWS SDK v2
func (s *SSMClientCreator) CreateClient() (*ssm.Client, error) {
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

	client := ssm.NewFromConfig(cfg)
	return client, err
}
