package flags

import (
	flag "github.com/spf13/pflag"
)

var (
	// FlagSet contains operator flags that are needed in multiple packages
	FlagSet *flag.FlagSet

	// Namespace is the namespace for the controller to be restricted to
	Namespace string

	AWS struct {
		Region                string
		AccessKey             string
		SecretKey             string
		Profile               string
		SharedCredentialsFile string
		Token                 string
		MaxRetries            int

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
)

func init() {
	FlagSet = flag.NewFlagSet("aws-resource-operator", flag.PanicOnError)

	FlagSet.StringVar(&Namespace, "namespace", "", "Only manage resources in given namespace")

	FlagSet.StringVar(&AWS.Region, "aws-region", "eu-central-1", "The region where AWS operations will take place, env AWS_DEFAULT_REGION or AWS_REGION")
	FlagSet.StringVar(&AWS.AccessKey, "aws-access-key", "", "The access key for API operations, env AWS_ACCESS_KEY_ID")
	FlagSet.StringVar(&AWS.SecretKey, "aws-secret-key", "", "The secret key for API operations, env AWS_SECRET_ACCESS_KEY")
	FlagSet.StringVar(&AWS.Profile, "aws-profile", "", "The profile for API operations, env AWS_PROFILE")
	FlagSet.StringVar(&AWS.SharedCredentialsFile, "aws-shared-credentials-file", "", "The path to the shared credentials file. If not set this defaults to ~/.aws/credentials, env AWS_SHARED_CREDENTIALS_FILE")

	FlagSet.StringVar(&AWS.Token, "aws-token", "", "Session token. A session token is only required if you are using temporary security credentials, env AWS_SESSION_TOKEN")
	FlagSet.IntVar(&AWS.MaxRetries, "aws-max-retries", 32, "The maximum number of times an AWS API request is being executed. If the API request still fails, an error is thrown")

	FlagSet.StringVar(&AWS.AssumeRoleARN, "aws-assume-role-arn", "", "The ARN of an IAM role to assume prior to making API calls")
	FlagSet.StringVar(&AWS.AssumeRoleExternalID, "aws-assume-role-external-id", "", "The external ID to use when assuming the role. If omitted, no external ID is passed to the AssumeRole call")
	FlagSet.StringVar(&AWS.AssumeRoleSessionName, "aws-assume-role-session-name", "", "The session name to use when assuming the role. If omitted, no session name is passed to the AssumeRole call")
	FlagSet.StringVar(&AWS.AssumeRolePolicy, "aws-assume-role-policy", "", "The permissions applied when assuming a role")

	FlagSet.StringSliceVar(&AWS.AllowedAccountIds, "aws-allowed-account-ids", []string{}, "List of allowed, white listed, AWS account IDs")
	FlagSet.StringSliceVar(&AWS.ForbiddenAccountIds, "aws-forbidden-account-ids", []string{}, "List of forbidden, blacklisted, AWS account IDs")

	FlagSet.StringVar(&AWS.IamEndpoint, "aws-iam-endpoint", "", "Overrides the default endpoint URL")
	FlagSet.StringVar(&AWS.S3Endpoint, "aws-s3-endpoint", "", "Overrides the default endpoint URL")
	FlagSet.StringVar(&AWS.S3ControlEndpoint, "aws-s3control-endpoint", "", "Overrides the default endpoint URL")
	FlagSet.StringVar(&AWS.StsEndpoint, "aws-sts-endpoint", "", "Overrides the default endpoint URL")

	FlagSet.BoolVar(&AWS.DebugSDK, "aws-debug-sdk", false, "Debug AWS SDK requests")
	FlagSet.BoolVar(&AWS.Insecure, "aws-insecure", false, "Explicitly allow the provider to perform insecure SSL requests")

	FlagSet.BoolVar(&AWS.SkipCredsValidation, "aws-skip-credentials-validation", false, "Skip the credentials validation via STS API")
	FlagSet.BoolVar(&AWS.SkipRegionValidation, "aws-skip-region-validation", false, "Skip static validation of region name")
	FlagSet.BoolVar(&AWS.SkipRequestingAccountId, "aws-skip-requesting-account-id", false, "Skip requesting the account ID")
	FlagSet.BoolVar(&AWS.SkipMetadataApiCheck, "aws-skip-metadata-api-check", false, "Skip the AWS Metadata API check")
	FlagSet.BoolVar(&AWS.S3ForcePathStyle, "aws-s3-force-path-style", false, "Set this to true to force the request to use path-style addressing")

}
