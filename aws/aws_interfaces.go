package aws

// CreateClient interface with a method to create an AWS client
type CreateClient interface {
	CreateClient() (interface{}, error) // Returns an interface{} so it can be flexible for different services
}
