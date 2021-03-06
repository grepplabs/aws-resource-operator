/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// S3BucketSpec defines the desired state of S3Bucket
type S3BucketSpec struct {
	// The name of the bucket
	Bucket string `json:"bucket"`
	// The region to use. Overrides config/env settings
	// +optional
	Region string `json:"region,omitempty"`
	// The canned ACL to apply. Defaults to "private"
	// +optional
	// +kubebuilder:validation:Enum=private,public-read,public-read-write,aws-exec-read,authenticated-read,bucket-owner-read,bucket-owner-full-control,log-delivery-write
	Acl string `json:"acl,omitempty"`
	// A valid bucket policy JSON document
	// +optional
	Policy string `json:"policy,omitempty"`
	// A mapping of tags to assign to the bucket
	// +optional
	Tags map[string]string `json:"tags,omitempty"`
	// The bucket delete strategy. Defaults to "Delete"
	// +optional
	// +kubebuilder:validation:Enum=Delete,Skip,Force
	DeleteStrategy DeleteStrategyType `json:"deleteStrategy,omitempty"`
	// The bucket ownership strategy. Defaults to "Created"
	// +optional
	// +kubebuilder:validation:Enum=Created,Acquire
	OwnershipStrategy OwnershipStrategyType `json:"ownershipStrategy,omitempty"`
	// A server-side encryption configuration
	// +optional
	ServerSideEncryptionConfiguration *S3BucketServerSideEncryptionConfiguration `json:"serverSideEncryptionConfiguration,omitempty"`
	// The bucket versioning state
	// +optional
	Versioning *S3BucketVersioning `json:"versioning,omitempty"`
	// The server access logging provides detailed records for the requests that are made to a bucket
	// +optional
	Logging *S3BucketLogging `json:"logging,omitempty"`
	// The static website hosting
	// +optional
	Website *S3BucketWebsite `json:"website,omitempty"`
	// The Cross-origin resource sharing (CORS)
	// +optional
	CORSConfiguration *S3BucketCORSConfiguration `json:"corsConfiguration,omitempty"`
}

type DeleteStrategyType string

const (
	// Delete the bucket
	DeleteDeleteStrategy DeleteStrategyType = "Delete"
	// Skip bucket deletion
	SkipDeleteStrategy DeleteStrategyType = "Skip"
	// All objects should be deleted from the bucket so that the bucket can be deleted without error
	ForceDeleteStrategy DeleteStrategyType = "Force"
)

type OwnershipStrategyType string

const (
	// Only buckets created by the operator are reconciled
	CreatedOwnershipStrategy OwnershipStrategyType = "Created"
	// The operator can reconcile existing buckets
	AcquireOwnershipStrategy OwnershipStrategyType = "Acquire"
)

// S3BucketStatus defines the observed state of S3Bucket
type S3BucketStatus struct {
	// The ARN of the S3 bucket
	ARN string `json:"arn,omitempty"`
	// The location constraint of S3 bucket
	LocationConstraint string `json:"locationConstraint,omitempty"`
	// The canned ACL
	Acl string `json:"acl,omitempty"`
}

// Container for server-side encryption configuration rules. Currently S3 supports one rule only.
type S3BucketServerSideEncryptionConfiguration struct {
	// Container for information about a particular server-side encryption configuration rule.
	Rule S3ServerSideEncryptionRule `json:"rule"`
}

// Container for information about a particular server-side encryption configuration rule.
type S3ServerSideEncryptionRule struct {
	// Describes the default server-side encryption to apply to new objects in the
	// bucket. If Put Object request does not specify any server-side encryption,
	// this default encryption will be applied.
	ApplyServerSideEncryptionByDefault S3ServerSideEncryptionByDefault `json:"applyServerSideEncryptionByDefault"`
}

// Describes the default server-side encryption to apply to new objects in the
// bucket. If Put Object request does not specify any server-side encryption,
// this default encryption will be applied.
type S3ServerSideEncryptionByDefault struct {
	// Server-side encryption algorithm to use for the default encryption. SSEAlgorithm is a required field.
	// +kubebuilder:validation:Enum=AES256,aws:kms
	SSEAlgorithm string `json:"sseAlgorithm"`
	// KMS master key ID to use for the default encryption. This parameter is allowed if SSEAlgorithm is aws:kms.
	// +optional
	KMSMasterKeyID string `json:"kmsMasterKeyID,omitempty"`
}

// Container for a state of versioning
type S3BucketVersioning struct {
	// Enable versioning. Once you version-enable a bucket, it can never return to an unversioned state.
	// You can, however, suspend versioning on that bucket
	Enabled bool `json:"enabled"`
}

// Container for logging information. Presence of this element indicates that logging is enabled.
type S3BucketLogging struct {
	// Specifies the bucket where you want Amazon S3 to store server access logs.
	// You can have your logs delivered to any bucket that you own, including the
	// same bucket that is being logged. You can also configure multiple buckets
	// to deliver their logs to the same target bucket. In this case you should
	// choose a different TargetPrefix for each source bucket so that the delivered
	// log files can be distinguished by key.
	TargetBucket string `json:"targetBucket"`
	// A prefix for Amazon S3 to assign to all log object keys.
	// +optional
	TargetPrefix string `json:"targetPrefix,omitempty"`
}

// Container for static website hosting.
type S3BucketWebsite struct {
	// This endpoint is used as a website address. Use this bucket to host a website
	// +optional
	Endpoint *S3BucketWebsiteEndpoint `json:"endpoint,omitempty"`

	// Redirect requests to bucket or target domain
	// +optional
	Redirect *S3BucketWebsiteRedirect `json:"redirect,omitempty"`
}

type S3BucketWebsiteEndpoint struct {
	// This endpoint is used as a website address.
	// Amazon S3 returns this index document when requests are made to the root domain or any of the subfolders.
	IndexDocument string `json:"indexDocument"`

	// This is returned when an error occures.
	// An absolute path to the document to return in case of a 4XX error.
	// +optional
	ErrorDocument string `json:"errorDocument,omitempty"`

	// A json array containing routing rules describing redirect behavior and when redirects are applied.
	// +optional
	RoutingRules string `json:"routingRules,omitempty"`
}

// Set up custom rules to automatically redirect webpage requests for specific content.
type S3BucketWebsiteRedirect struct {
	// Name of the host where requests will be redirected, target bucket or domain
	HostName string `json:"hostName"`

	// Protocol to use (http, https) when redirecting requests. The default is the protocol that is used in the original request.
	// +optional
	// +kubebuilder:validation:Enum=http,https
	Protocol string `json:"protocol,omitempty"`
}

// Container for CORS configuration rules.
type S3BucketCORSConfiguration struct {
	// CORS rules
	CORSRules []S3BucketCORSRule `json:"corsRules"`
}

// Cross-origin resource sharing (CORS)
type S3BucketCORSRule struct {
	// Specifies which headers are allowed in a pre-flight OPTIONS request.
	// +optional
	AllowedHeaders []string `json:"allowedHeaders,omitempty"`

	// Identifies HTTP methods that the domain/origin specified in the rule is allowed
	// to execute.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:Enum=GET,PUT,POST,DELETE,HEAD
	AllowedMethods []string `json:"allowedMethods"`

	// One or more origins you want customers to be able to access the bucket from.
	// +kubebuilder:validation:MinItems=1
	AllowedOrigins []string `json:"allowedOrigins"`

	// One or more headers in the response that you want customers to be able to
	// access from their applications (for example, from a JavaScript XMLHttpRequest object).
	// +optional
	ExposeHeaders []string `json:"exposeHeaders,omitempty"`

	// The time in seconds that your browser is to cache the preflight response
	// for the specified resource.
	// +optional
	MaxAgeSeconds *int64 `json:"maxAgeSeconds,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// S3Bucket is the Schema for the s3buckets API
// +k8s:openapi-gen=true
type S3Bucket struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   S3BucketSpec   `json:"spec,omitempty"`
	Status S3BucketStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// S3BucketList contains a list of S3Bucket
type S3BucketList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []S3Bucket `json:"items"`
}

func init() {
	SchemeBuilder.Register(&S3Bucket{}, &S3BucketList{})
}
