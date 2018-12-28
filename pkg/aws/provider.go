package aws

import (
	opflags "github.com/grepplabs/aws-resource-operator/pkg/flags"
	"github.com/mitchellh/go-homedir"
	"os"
	"sync"
)

var (
	awsClient     *AWSClient
	awsClientOnce sync.Once
)

func GetAWSClient() *AWSClient {
	awsClientOnce.Do(func() {
		var err error
		awsClient, err = newAWSClient()
		if err != nil {
			log.Error(err, "[FATAL] Failed to create AWS Client")
			os.Exit(1)
		}
	})
	return awsClient
}

func newAWSClient() (*AWSClient, error) {
	log.Info("Creating a new AWS Client")

	config := Config{
		AccessKey:               opflags.AWS.AccessKey,
		SecretKey:               opflags.AWS.SecretKey,
		Profile:                 opflags.AWS.Profile,
		Token:                   opflags.AWS.Token,
		Region:                  opflags.AWS.Region,
		MaxRetries:              opflags.AWS.MaxRetries,
		Insecure:                opflags.AWS.Insecure,
		DebugSDK:                opflags.AWS.DebugSDK,
		AssumeRoleARN:           opflags.AWS.AssumeRoleARN,
		AssumeRoleExternalID:    opflags.AWS.AssumeRoleExternalID,
		AssumeRoleSessionName:   opflags.AWS.AssumeRoleSessionName,
		AssumeRolePolicy:        opflags.AWS.AssumeRolePolicy,
		AllowedAccountIds:       opflags.AWS.AllowedAccountIds,
		ForbiddenAccountIds:     opflags.AWS.ForbiddenAccountIds,
		IamEndpoint:             opflags.AWS.IamEndpoint,
		S3Endpoint:              opflags.AWS.S3Endpoint,
		S3ControlEndpoint:       opflags.AWS.S3ControlEndpoint,
		StsEndpoint:             opflags.AWS.StsEndpoint,
		SkipCredsValidation:     opflags.AWS.SkipCredsValidation,
		SkipRegionValidation:    opflags.AWS.SkipRegionValidation,
		SkipRequestingAccountId: opflags.AWS.SkipRequestingAccountId,
		SkipMetadataApiCheck:    opflags.AWS.SkipMetadataApiCheck,
		S3ForcePathStyle:        opflags.AWS.S3ForcePathStyle,
	}

	credsPath, err := homedir.Expand(opflags.AWS.SharedCredentialsFile)
	if err != nil {
		return nil, err
	}
	config.CredsFilename = credsPath

	if config.AssumeRoleARN != "" {
		logInfof("AssumeRole configuration set ARN %s (SessionName: %q, ExternalID: %q, Policy: %q)", config.AssumeRoleARN, config.AssumeRoleSessionName, config.AssumeRoleExternalID, config.AssumeRolePolicy)

	} else {
		log.Info("No AssumeRole parameters read from configuration")
	}
	return config.Client()
}
