// cmd/root.go
package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	myaws "github.com/Otter2022/cloud-native-mpi-for-aws-cli/aws"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/spf13/cobra"
)

var (
	numInstances int
	vpcID        string
	filePath     string
)

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

	// Step 3: Assign ranks and set up environment variables
	err = setupEnvironment(selectedInstances)
	if err != nil {
		fmt.Printf("Error setting up environment: %v\n", err)
		os.Exit(1)
	}

	// Step 4: Distribute the Go program
	err = distributeProgram(selectedInstances)
	if err != nil {
		fmt.Printf("Error distributing program: %v\n", err)
		os.Exit(1)
	}

	// Step 5: Execute the program on all instances
	err = executeProgram(selectedInstances)
	if err != nil {
		fmt.Printf("Error executing program: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Program executed successfully on all instances.")
}

func discoverInstances(vpcID string) ([]myaws.InstanceInfo, error) {
	// Initialize EC2 client
	ec2ClientCreator := myaws.EC2ClientCreator{}
	ec2Client, err := ec2ClientCreator.CreateClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create EC2 client: %v", err)
	}

	// Describe instances with filters
	input := &ec2.DescribeInstancesInput{
		Filters: []types.Filter{
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

	var instances []myaws.InstanceInfo
	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			if instance.InstanceId != nil && instance.PrivateIpAddress != nil {
				instances = append(instances, myaws.InstanceInfo{
					InstanceID: *instance.InstanceId,
					PrivateIP:  *instance.PrivateIpAddress,
				})
			}
		}
	}

	return instances, nil
}

func setupEnvironment(instances []myaws.InstanceInfo) error {
	n := len(instances)

	// Assign ranks and set environment variables
	for i := 0; i < n; i++ {
		instances[i].InstanceRank = i
	}

	// Initialize SSM client
	ssmClientCreator := myaws.SSMClientCreator{}
	ssmClient, err := ssmClientCreator.CreateClient()
	if err != nil {
		return fmt.Errorf("failed to create SSM client: %v", err)
	}

	// Prepare environment variables for each instance
	return InitializeEnvironments(ssmClient, instances)
}

// Remove the Build part since we're only setting up the environment
func InitializeEnvironments(client *ssm.Client, instances []myaws.InstanceInfo) error {
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
				envVars = append(envVars, fmt.Sprintf("export MPI_ADDRESS_%d=\"%s:50051\"", x, instances[x].PrivateIP))
			}

			// Commands to set environment variables
			allCommands := strings.Join(envVars, "\n")

			finalScript := fmt.Sprintf("#!/bin/bash\n%v\n", allCommands)

			input := &ssm.SendCommandInput{
				DocumentName: aws.String("myaws-RunShellScript"),
				Parameters: map[string][]string{
					"commands": {finalScript},
				},
				InstanceIds:    []string{instances[i].InstanceID},
				TimeoutSeconds: aws.Int32(600),
			}
			_, err := client.SendCommand(context.TODO(), input)
			if err != nil {
				fmt.Printf("Failed to send command to instance %s: %v\n", instances[i].InstanceID, err)
				mu.Lock()
				errorsOccurred = true
				mu.Unlock()
				return
			}
		}(i)
	}

	wg.Wait()

	if errorsOccurred {
		return fmt.Errorf("errors occurred during environment setup")
	}

	return nil
}

func distributeProgram(instances []myaws.InstanceInfo) error {
	// Initialize S3 client
	s3Bucket := "your-s3-bucket-name" // Replace with your bucket name
	s3Client, err := myaws.NewS3Client(s3Bucket)
	if err != nil {
		return fmt.Errorf("failed to create S3 client: %v", err)
	}

	// Upload the Go program to S3
	s3Key := "mpi_program.go"
	err = s3Client.UploadFile(filePath, s3Key)
	if err != nil {
		return fmt.Errorf("failed to upload Go program to S3: %v", err)
	}

	// Use SSM to download the program on each instance
	ssmClientCreator := myaws.SSMClientCreator{}
	ssmClient, err := ssmClientCreator.CreateClient()
	if err != nil {
		return fmt.Errorf("failed to create SSM client: %v", err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	errorsOccurred := false

	for _, instance := range instances {
		wg.Add(1)
		go func(instance myaws.InstanceInfo) {
			defer wg.Done()
			script := fmt.Sprintf(`#!/bin/bash
cd /home/ec2-user
myaws s3 cp s3://%s/%s mpi_program.go
`, s3Bucket, s3Key)

			input := &ssm.SendCommandInput{
				DocumentName: aws.String("myaws-RunShellScript"),
				Parameters: map[string][]string{
					"commands": {script},
				},
				InstanceIds:    []string{instance.InstanceID},
				TimeoutSeconds: aws.Int32(600),
			}
			_, err := ssmClient.SendCommand(context.TODO(), input)
			if err != nil {
				fmt.Printf("Failed to send command to instance %s: %v\n", instance.InstanceID, err)
				mu.Lock()
				errorsOccurred = true
				mu.Unlock()
				return
			}
		}(instance)
	}

	wg.Wait()

	if errorsOccurred {
		return fmt.Errorf("errors occurred during program distribution")
	}

	return nil
}

func executeProgram(instances []myaws.InstanceInfo) error {
	ssmClientCreator := myaws.SSMClientCreator{}
	ssmClient, err := ssmClientCreator.CreateClient()
	if err != nil {
		return fmt.Errorf("failed to create SSM client: %v", err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	errorsOccurred := false

	for _, instance := range instances {
		wg.Add(1)
		go func(instance myaws.InstanceInfo) {
			defer wg.Done()
			script := `#!/bin/bash
cd /home/ec2-user
go build -o mpi_program mpi_program.go
./mpi_program > output.txt 2>&1
`

			input := &ssm.SendCommandInput{
				DocumentName: aws.String("myaws-RunShellScript"),
				Parameters: map[string][]string{
					"commands": {script},
				},
				InstanceIds:    []string{instance.InstanceID},
				TimeoutSeconds: aws.Int32(600),
			}
			_, err := ssmClient.SendCommand(context.TODO(), input)
			if err != nil {
				fmt.Printf("Failed to execute program on instance %s: %v\n", instance.InstanceID, err)
				mu.Lock()
				errorsOccurred = true
				mu.Unlock()
				return
			}
		}(instance)
	}

	wg.Wait()

	if errorsOccurred {
		return fmt.Errorf("errors occurred during program execution")
	}

	return nil
}

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:   "awsmpirun",
	Short: "Distribute and run MPI-like programs on myaws EC2 instances",
	Long: `awsmpirun is a CLI tool that distributes a Go program across existing EC2 instances
in a VPC subnet, assigns ranks, and sets up environment variables for MPI-like communication.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Call the function to run your application
		runAWSMPIRun()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
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
	rootCmd.Flags().StringVarP(&filePath, "file", "f", "", "Path to the Go file to run (required)")

	// Mark required flags
	rootCmd.MarkFlagRequired("vpc")
	rootCmd.MarkFlagRequired("file")
}
