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
