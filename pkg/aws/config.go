package aws

import (
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/grepplabs/aws-resource-operator/pkg/version"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3control"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/go-cleanhttp"
)

type Config struct {
	AccessKey     string
	SecretKey     string
	CredsFilename string
	Profile       string
	Token         string
	Region        string
	MaxRetries    int

	AssumeRoleARN         string
	AssumeRoleExternalID  string
	AssumeRoleSessionName string
	AssumeRolePolicy      string

	AllowedAccountIds   []string
	ForbiddenAccountIds []string

	IamEndpoint       string
	S3Endpoint        string
	S3ControlEndpoint string
	StsEndpoint       string

	DebugSDK bool
	Insecure bool

	SkipCredsValidation     bool
	SkipRegionValidation    bool
	SkipRequestingAccountId bool
	SkipMetadataApiCheck    bool
	S3ForcePathStyle        bool
}

type AWSClient struct {
	region    string
	partition string
	accountid string

	iamconn       *iam.IAM
	stsconn       *sts.STS
	s3conn        *s3.S3
	s3controlconn *s3control.S3Control
}

func (c *AWSClient) S3() *s3.S3 {
	return c.s3conn
}

func (c *AWSClient) Partition() string {
	return c.partition
}

func (c *AWSClient) Region() string {
	return c.region
}

func (c *AWSClient) Account() string {
	return c.accountid
}

// Client configures and returns a fully initialized AWSClient
func (c *Config) Client() (*AWSClient, error) {
	// Get the auth and region. This can fail if keys/regions were not
	// specified and we're attempting to use the environment.
	if c.SkipRegionValidation {
		log.Info("Skipping region validation")
	} else {
		log.Info("Building AWS region structure")
		err := c.ValidateRegion()
		if err != nil {
			return nil, err
		}
	}

	var client AWSClient
	// store AWS region in client struct, for region specific operations such as
	// bucket storage in S3
	client.region = c.Region

	log.Info("Building AWS auth structure")
	creds, err := GetCredentials(c)
	if err != nil {
		return nil, err
	}

	// define the AWS Session options
	// Credentials or Profile will be set in the Options below
	// MaxRetries may be set once we validate credentials
	var opt = session.Options{
		Config: aws.Config{
			Region:           aws.String(c.Region),
			MaxRetries:       aws.Int(0),
			HTTPClient:       cleanhttp.DefaultClient(),
			S3ForcePathStyle: aws.Bool(c.S3ForcePathStyle),
		},
	}

	// Call Get to check for credential provider. If nothing found, we'll get an
	// error, and we can present it nicely to the user
	cp, err := creds.Get()
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == "NoCredentialProviders" {
			// If a profile wasn't specified, the session may still be able to resolve credentials from shared config.
			if c.Profile == "" {
				sess, err := session.NewSession()
				if err != nil {
					return nil, errors.New("no valid credential sources found for AWS Provider")
				}
				_, err = sess.Config.Credentials.Get()
				if err != nil {
					return nil, errors.New("no valid credential sources found for AWS Provider")
				}
				logInfof("Using session-derived AWS Auth")
				opt.Config.Credentials = sess.Config.Credentials
			} else {
				logInfof("AWS Auth using Profile: %s", c.Profile)
				opt.Profile = c.Profile
				opt.SharedConfigState = session.SharedConfigEnable
			}
		} else {
			return nil, fmt.Errorf("error loading credentials for AWS Provider: %s", err)
		}
	} else {
		// add the validated credentials to the session options
		logInfof("AWS Auth provider used: %s", cp.ProviderName)
		opt.Config.Credentials = creds
	}

	if c.DebugSDK {
		opt.Config.LogLevel = aws.LogLevel(aws.LogDebugWithHTTPBody | aws.LogDebugWithRequestRetries | aws.LogDebugWithRequestErrors)
		opt.Config.Logger = awsLogger{}
	}

	if c.Insecure {
		transport := opt.Config.HTTPClient.Transport.(*http.Transport)
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}

	// create base session with no retries. MaxRetries will be set later
	sess, err := session.NewSessionWithOptions(opt)
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == "NoCredentialProviders" {
			return nil, errors.New("no valid credential sources found for AWS Provider")
		}
		return nil, fmt.Errorf("error creating AWS session: %s", err)
	}

	sess.Handlers.Build.PushBackNamed(addOperatorVersionToUserAgent)

	if extraDebug := os.Getenv("OPERATOR_AWS_AUTH_FAILURE_DEBUG"); extraDebug != "" {
		sess.Handlers.UnmarshalError.PushFrontNamed(debugAuthFailure)
	}

	// if the desired number of retries is non-zero, update the session
	if c.MaxRetries > 0 {
		sess = sess.Copy(&aws.Config{MaxRetries: aws.Int(c.MaxRetries)})
	}

	// Generally, we want to configure a lower retry theshold for networking issues
	// as the session retry threshold is very high by default and can mask permanent
	// networking failures, such as a non-existent service endpoint.
	// MaxRetries will override this logic if it has a lower retry threshold.
	// NOTE: This logic can be fooled by other request errors raising the retry count
	//       before any networking error occurs
	sess.Handlers.Retry.PushBack(func(r *request.Request) {
		// We currently depend on the DefaultRetryer exponential backoff here.
		// ~10 retries gives a fair backoff of a few seconds.
		if r.RetryCount < 9 {
			return
		}
		// RequestError: send request failed
		// caused by: Post https://FQDN/: dial tcp: lookup FQDN: no such host
		if IsAWSErrExtended(r.Error, "RequestError", "send request failed", "no such host") {
			log.Info("[WARN] Disabling retries after next request due to networking issue")
			r.Retryable = aws.Bool(false)
		}
		// RequestError: send request failed
		// caused by: Post https://FQDN/: dial tcp IPADDRESS:443: connect: connection refused
		if IsAWSErrExtended(r.Error, "RequestError", "send request failed", "connection refused") {
			log.Info("[WARN] Disabling retries after next request due to networking issue")
			r.Retryable = aws.Bool(false)
		}
	})

	awsIamSess := sess.Copy(&aws.Config{Endpoint: aws.String(c.IamEndpoint)})
	awsS3Sess := sess.Copy(&aws.Config{Endpoint: aws.String(c.S3Endpoint)})
	awsS3ControlSess := sess.Copy(&aws.Config{Endpoint: aws.String(c.S3ControlEndpoint)})
	awsStsSess := sess.Copy(&aws.Config{Endpoint: aws.String(c.StsEndpoint)})

	client.iamconn = iam.New(awsIamSess)
	client.stsconn = sts.New(awsStsSess)

	if c.AssumeRoleARN != "" {
		client.accountid, client.partition, _ = parseAccountIDAndPartitionFromARN(c.AssumeRoleARN)
	}

	// Validate credentials early and fail before we do any graph walking.
	if !c.SkipCredsValidation {
		var err error
		client.accountid, client.partition, err = GetAccountIDAndPartitionFromSTSGetCallerIdentity(client.stsconn)
		if err != nil {
			return nil, fmt.Errorf("error validating provider credentials: %s", err)
		}
	}

	if client.accountid == "" && !c.SkipRequestingAccountId {
		var err error
		client.accountid, client.partition, err = GetAccountIDAndPartition(client.iamconn, client.stsconn, cp.ProviderName)
		if err != nil {
			return nil, fmt.Errorf("AWS account ID not previously found and failed retrieving via all available methods: %s", err)
		}
	}

	if client.accountid == "" {
		log.Info("[WARN] AWS account ID not found for provider")
	}

	authErr := c.ValidateAccountId(client.accountid)
	if authErr != nil {
		return nil, authErr
	}

	// Infer AWS partition from configured region if we still need it
	if client.partition == "" {
		if partition, ok := endpoints.PartitionForRegion(endpoints.DefaultPartitions(), client.region); ok {
			client.partition = partition.ID()
		}
	}

	client.s3conn = s3.New(awsS3Sess)
	client.s3controlconn = s3control.New(awsS3ControlSess)

	return &client, nil
}

// ValidateRegion returns an error if the configured region is not a
// valid aws region and nil otherwise.
func (c *Config) ValidateRegion() error {
	for _, partition := range endpoints.DefaultPartitions() {
		for _, region := range partition.Regions() {
			if c.Region == region.ID() {
				return nil
			}
		}
	}

	return fmt.Errorf("not a valid region: %s", c.Region)
}

// ValidateAccountId returns a context-specific error if the configured account
// id is explicitly forbidden or not authorised; and nil if it is authorised.
func (c *Config) ValidateAccountId(accountId string) error {

	logInfof("Validating account ID: %s", accountId)

	if c.ForbiddenAccountIds != nil && len(c.ForbiddenAccountIds) > 0 {
		for _, id := range c.ForbiddenAccountIds {
			if id == accountId {
				return fmt.Errorf("forbidden account ID (%s)", id)
			}
		}
	}

	if c.AllowedAccountIds != nil && len(c.AllowedAccountIds) > 0 {
		for _, id := range c.AllowedAccountIds {
			if id == accountId {
				return nil
			}
		}
		return fmt.Errorf("account ID not allowed (%s)", accountId)
	}

	return nil
}

// addOperatorVersionToUserAgent is a named handler that will add AWS Resource Operator's
// version information to requests made by the AWS SDK.
var addOperatorVersionToUserAgent = request.NamedHandler{
	Name: "aws-resource-operator.AWSResourceOperatorVersionUserAgentHandler",
	Fn: request.MakeAddToUserAgentHandler(
		"aws-resource-operator", version.Version),
}

var debugAuthFailure = request.NamedHandler{
	Name: "aws-resource-operator.AuthFailureAdditionalDebugHandler",
	Fn: func(req *request.Request) {
		if IsAWSErr(req.Error, "AuthFailure", "AWS was not able to validate the provided access credentials") {
			log.Info("[DEBUG] Additional AuthFailure Debugging Context")
			logInfof("[DEBUG] Current system UTC time: %s", time.Now().UTC())
			logInfof("[DEBUG] Request object: %s", spew.Sdump(req))
		}
	},
}

type awsLogger struct{}

func (l awsLogger) Log(args ...interface{}) {
	tokens := make([]string, 0, len(args))
	for _, arg := range args {
		if token, ok := arg.(string); ok {
			tokens = append(tokens, token)
		}
	}
	logInfof("[DEBUG] [aws-sdk-go] %s", strings.Join(tokens, " "))
}
