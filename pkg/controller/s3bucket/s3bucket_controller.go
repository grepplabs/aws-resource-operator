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

package s3bucket

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"reflect"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	awsv1beta1 "github.com/grepplabs/aws-resource-operator/pkg/apis/aws/v1beta1"
	awsclient "github.com/grepplabs/aws-resource-operator/pkg/aws"
	"github.com/grepplabs/aws-resource-operator/pkg/helper/structure"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	deleteBucketFinalizerName = "delete-bucket.finalizers.aws.grepplabs.com"
)

var (
	log = logf.Log.WithName("s3bucket-controller")
	// extra pattern from s3err/error.go
	s3ErrorPattern = regexp.MustCompile(`(?m:status code: .*, request id: .*, host id: .*)`)
)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new S3Bucket Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileS3Bucket{
		Client:    mgr.GetClient(),
		scheme:    mgr.GetScheme(),
		recorder:  mgr.GetRecorder("s3bucket-controller"),
		partition: awsclient.GetAWSClient().Partition(),
		region:    awsclient.GetAWSClient().Region(),
		s3conn:    awsclient.GetAWSClient().S3(),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("s3bucket-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to S3Bucket
	err = c.Watch(&source.Kind{Type: &awsv1beta1.S3Bucket{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileS3Bucket{}

// ReconcileS3Bucket reconciles a S3Bucket object
type ReconcileS3Bucket struct {
	client.Client
	scheme   *runtime.Scheme
	recorder record.EventRecorder

	region    string
	partition string
	s3conn    s3iface.S3API
}

// Reconcile reads that state of the cluster for a S3Bucket object and makes changes based on the state read
// and what is in the S3Bucket.Spec
// a Deployment as an example
// Automatically generate RBAC rules to allow the Controller to read and write Deployments
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=aws.grepplabs.com,resources=s3buckets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=aws.grepplabs.com,resources=s3buckets/status,verbs=get;update;patch
func (r *ReconcileS3Bucket) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// Fetch the S3Bucket instance
	instance := &awsv1beta1.S3Bucket{}
	err := r.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if instance.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object.
		if !structure.ContainsString(instance.ObjectMeta.Finalizers, deleteBucketFinalizerName) {
			instance.ObjectMeta.Finalizers = append(instance.ObjectMeta.Finalizers, deleteBucketFinalizerName)
			if err := r.Update(context.Background(), instance); err != nil {
				return reconcile.Result{Requeue: true}, nil
			}
		}
	} else {
		// The object is being deleted
		if structure.ContainsString(instance.ObjectMeta.Finalizers, deleteBucketFinalizerName) {
			// our finalizer is present, so lets handle our external dependency
			if retryError := r.deleteBucket(instance); retryError != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return r.handleRetryError(instance, retryError)
			}

			// remove our finalizer from the list and update it.
			instance.ObjectMeta.Finalizers = structure.RemoveString(instance.ObjectMeta.Finalizers, deleteBucketFinalizerName)
			if err := r.Update(context.Background(), instance); err != nil {
				return reconcile.Result{Requeue: true}, nil
			}
		}
		// Our finalizer has finished, so the reconciler can do nothing.
		return reconcile.Result{}, nil
	}

	// Validate parameters before reconcile
	retryError := r.validateInstance(instance)
	if retryError != nil {
		return r.handleRetryError(instance, retryError)
	}

	retryError = r.reconcileInstance(request, instance)
	if retryError != nil {
		return r.handleRetryError(instance, retryError)
	}
	return reconcile.Result{}, nil
}

func (r ReconcileS3Bucket) handleRetryError(instance *awsv1beta1.S3Bucket, retryError *awsclient.RetryError) (reconcile.Result, error) {
	if retryError.Reason != "" {
		r.sendEvent(instance, apiv1.EventTypeWarning, retryError.Reason, eventMessageFromError(retryError.Err))
	}
	err := retryError.Err
	if err == nil {
		err = fmt.Errorf("error cause not specified, reason: %s", retryError.Reason)
	}
	return reconcile.Result{Requeue: retryError.Retryable}, err
}

func eventMessageFromError(err error) string {
	errorString := fmt.Sprintf("%s", err)
	loc := s3ErrorPattern.FindStringIndex(errorString)
	var msg string
	if loc != nil {
		msg = strings.TrimSuffix(strings.TrimSpace(errorString[:loc[0]]), `\n\t`)
	}
	if msg != "" {
		return msg
	}
	return "Reconcile failed"
}

func (r ReconcileS3Bucket) validateInstance(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	bucket := instance.Spec.Bucket
	region := r.bucketRegion(instance)

	if err := validateBucketName(bucket, region); err != nil {
		return awsclient.NonRetryableError(fmt.Errorf("error validating S3 bucket name: %s", err), "BucketNameValidation")
	}
	if instance.Spec.Logging != nil {
		if instance.Spec.Logging.TargetBucket == "" {
			return awsclient.NonRetryableError(fmt.Errorf("error validating S3 bucket Logging: TargetBucket is required"), "BucketLoggingValidation")
		}
	}
	if instance.Spec.Website != nil {
		if instance.Spec.Website.Redirect != nil && instance.Spec.Website.Endpoint != nil {
			return awsclient.NonRetryableError(fmt.Errorf("error validating S3 bucket Website: Only Redirect OR Endpoint can be set"), "BucketWebsiteValidation")
		}
		if instance.Spec.Website.Redirect == nil && instance.Spec.Website.Endpoint == nil {
			return awsclient.NonRetryableError(fmt.Errorf("error validating S3 bucket Website: Redirect OR Endpoint must be set"), "BucketWebsiteValidation")
		}
	}
	if instance.Spec.CORSConfiguration != nil && len(instance.Spec.CORSConfiguration.CORSRules) == 0 {
		return awsclient.NonRetryableError(fmt.Errorf("error validating S3 bucket CORS: corsRules are empty"), "BucketWebsiteValidation")
	}
	if instance.Status.ARN != "" {
		bucketARN := r.bucketARN(instance)
		if instance.Status.ARN != bucketARN {
			return awsclient.NonRetryableError(fmt.Errorf("bucket name changed"), "BucketNameChanged")
		}
	}
	if instance.Status.LocationConstraint != "" {
		bucketRegion := r.bucketRegion(instance)
		if instance.Status.LocationConstraint != bucketRegion {
			return awsclient.NonRetryableError(fmt.Errorf("bucket region changed"), "BucketRegionChanged")
		}
	}
	return nil
}

// https://docs.aws.amazon.com/AmazonS3/latest/dev/BucketRestrictions.html
func validateBucketName(bucket string, region string) error {
	if region != "us-east-1" {
		if (len(bucket) < 3) || (len(bucket) > 63) {
			return fmt.Errorf("%q must contain from 3 to 63 characters", bucket)
		}
		if !regexp.MustCompile(`^[0-9a-z-.]+$`).MatchString(bucket) {
			return fmt.Errorf("only lowercase alphanumeric characters and hyphens allowed in %q", bucket)
		}
		if regexp.MustCompile(`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$`).MatchString(bucket) {
			return fmt.Errorf("%q must not be formatted as an IP address", bucket)
		}
		if strings.HasPrefix(bucket, `.`) {
			return fmt.Errorf("%q cannot start with a period", bucket)
		}
		if strings.HasSuffix(bucket, `.`) {
			return fmt.Errorf("%q cannot end with a period", bucket)
		}
		if strings.Contains(bucket, `..`) {
			return fmt.Errorf("%q can be only one period between labels", bucket)
		}
	} else {
		if len(bucket) > 255 {
			return fmt.Errorf("%q must contain less than 256 characters", bucket)
		}
		if !regexp.MustCompile(`^[0-9a-zA-Z-._]+$`).MatchString(bucket) {
			return fmt.Errorf("only alphanumeric characters, hyphens, periods, and underscores allowed in %q", bucket)
		}
	}
	return nil
}

func (r ReconcileS3Bucket) bucketARN(instance *awsv1beta1.S3Bucket) string {
	return arn.ARN{
		Partition: r.partition,
		Service:   "s3",
		Resource:  instance.Spec.Bucket,
	}.String()
}

func (r ReconcileS3Bucket) bucketRegion(instance *awsv1beta1.S3Bucket) string {
	if instance.Spec.Region != "" {
		return instance.Spec.Region
	}
	return r.region
}
func (r ReconcileS3Bucket) bucketAcl(instance *awsv1beta1.S3Bucket) string {
	if instance.Spec.Acl != "" {
		return instance.Spec.Acl
	}
	return "private"
}

func (r ReconcileS3Bucket) reconcileInstance(request reconcile.Request, instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	log.Info("Reconcile S3 bucket", "bucket", instance.Spec.Bucket, "name", instance.Name)

	bucketExists, retryError := r.checkBucketExists(instance)
	if retryError != nil {
		return retryError
	}
	if !bucketExists {
		if retryError = r.createBucket(instance); retryError != nil {
			return retryError
		}
	}

	// check created by operator
	if instance.Status.ARN == "" {
		if instance.Spec.OwnershipStrategy == awsv1beta1.AcquireOwnershipStrategy {
			if retryError = r.acquireBucketOwnership(instance); retryError != nil {
				return retryError
			}
		} else {
			log.Info("[WARN] Cannot reconcile not own S3 bucket", "ownershipStrategy", instance.Spec.OwnershipStrategy)
			return awsclient.NonRetryableError(fmt.Errorf("cannot reconcile not own S3 bucket, use ownershipStrategy '%s' to force bucket management", awsv1beta1.AcquireOwnershipStrategy), "BucketNotOwned")
		}
	}
	return r.reconcileBucket(instance)
}

func (r ReconcileS3Bucket) acquireBucketOwnership(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	bucket := instance.Spec.Bucket
	bucketArn := r.bucketARN(instance)
	region := r.bucketRegion(instance)

	log.Info("Acquiring S3 bucket ownership", "bucket", bucket, "region", region)

	// get bucket location
	getBucketLocationOutput, err := r.s3conn.GetBucketLocation(&s3.GetBucketLocationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
			return awsclient.RetryableError(err, "GetBucketLocationFailed")
		}
		return awsclient.NonRetryableError(fmt.Errorf("error reading S3 Bucket location (%s): %s", bucket, err), "GetBucketLocationFailed")
	}

	// update status
	log.Info("Updating status of acquired S3 bucket", "bucket", bucket)
	if getBucketLocationOutput.LocationConstraint != nil {
		if region != *getBucketLocationOutput.LocationConstraint {
			errorString := fmt.Sprintf("location constraint configuration of S3Bucket changed from %s to %s", *getBucketLocationOutput.LocationConstraint, region)
			log.Info("[ERROR] "+errorString, "bucket", instance.Spec.Bucket)
			return awsclient.NonRetryableError(fmt.Errorf(errorString), "AcquireBucketOwnershipFailed")
		}
		instance.Status.LocationConstraint = *getBucketLocationOutput.LocationConstraint
	}
	instance.Status.ARN = bucketArn

	if retryError := r.updateInstance(instance); retryError != nil {
		return retryError
	}
	r.sendEvent(instance, apiv1.EventTypeNormal, "BucketOwnershipAcquired", "S3 bucket %s", bucketArn)

	return nil
}

func (r ReconcileS3Bucket) checkBucketExists(instance *awsv1beta1.S3Bucket) (bool, *awsclient.RetryError) {
	bucket := instance.Spec.Bucket
	// check bucket exists
	_, err := r.s3conn.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
			return false, awsclient.RetryableError(err, "HeadBucketFailed")
		}
		if awsError, ok := err.(awserr.RequestFailure); ok && awsError.StatusCode() == 404 {
			log.Info("S3 Bucket not found", "bucket", bucket)
			return false, nil
		}
		return false, awsclient.NonRetryableError(fmt.Errorf("error reading S3 Bucket (%s): %s", bucket, err), "HeadBucketFailed")
	}
	return true, nil
}

func (r ReconcileS3Bucket) reconcileBucket(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	if err := r.reconcileBucketTags(instance); err != nil {
		return err
	}
	if err := r.reconcileBucketPolicy(instance); err != nil {
		return err
	}
	if err := r.reconcileBucketAcl(instance); err != nil {
		return err
	}
	if err := r.reconcileBucketEncryption(instance); err != nil {
		return err
	}
	if err := r.reconcileBucketVersioning(instance); err != nil {
		return err
	}
	if err := r.reconcileBucketLogging(instance); err != nil {
		return err
	}
	if err := r.reconcileBucketWebsite(instance); err != nil {
		return err
	}
	if err := r.reconcileBucketCORS(instance); err != nil {
		return err
	}
	if err := r.reconcileBucketLifecycle(instance); err != nil {
		return err
	}
	return nil
}
func (r ReconcileS3Bucket) reconcileBucketTags(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	bucket := instance.Spec.Bucket
	currentTags, retryError := r.readBucketTags(bucket)
	if retryError != nil {
		return retryError
	}
	desiredTags := instance.Spec.Tags
	if reflect.DeepEqual(currentTags, desiredTags) {
		return nil
	}
	create, remove := diffTagsS3(currentTags, desiredTags)
	if len(remove) > 0 {
		logInfof("Delete bucket tagging (%s):\n%s", bucket, currentTags)
		_, err := r.s3conn.DeleteBucketTagging(&s3.DeleteBucketTaggingInput{Bucket: aws.String(bucket)})
		if err != nil {
			if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket, "OperationAborted"}) {
				return awsclient.RetryableError(err, "DeleteBucketTaggingFailed")
			}
			return awsclient.NonRetryableError(fmt.Errorf("error deleting S3 Bucket tagging (%s): %s", bucket, err), "DeleteBucketTaggingFailed")
		}
	}
	if len(create) > 0 {
		logInfof("Put bucket tagging (%s): %s", bucket, desiredTags)
		_, err := r.s3conn.PutBucketTagging(&s3.PutBucketTaggingInput{
			Bucket:  aws.String(bucket),
			Tagging: &s3.Tagging{TagSet: tagsFromMapS3(create)},
		})
		if err != nil {
			if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket, "OperationAborted"}) {
				return awsclient.RetryableError(err, "PutBucketTaggingFailed")
			}
			return awsclient.NonRetryableError(fmt.Errorf("error putting S3 Bucket tagging (%s): %s", bucket, err), "PutBucketTaggingFailed")
		}
	}
	if len(remove) > 0 || len(create) > 0 {
		var reason string
		if len(create) > 0 && len(remove) == 0 {
			reason = "BucketTaggingCreated"
		} else if len(create) == 0 && len(remove) > 0 {
			reason = "BucketTaggingDeleted"
		} else {
			reason = "BucketTaggingUpdated"
		}
		r.sendEvent(instance, apiv1.EventTypeNormal, reason, "Bucket tagging changed")
	}
	return nil
}

func (r ReconcileS3Bucket) readBucketTags(bucket string) (map[string]string, *awsclient.RetryError) {
	getBucketTaggingOutput, err := r.s3conn.GetBucketTagging(&s3.GetBucketTaggingInput{
		Bucket: aws.String(bucket),
	})
	if ec2err, ok := err.(awserr.Error); ok && ec2err.Code() == "NoSuchTagSet" {
		// There is no tag set associated with the bucket.
		return map[string]string{}, nil
	} else if err != nil {
		return nil, awsclient.NonRetryableError(fmt.Errorf("error reading S3 Bucket tags (%s): %s", bucket, err), "GetBucketTaggingFailed")
	}
	return tagsToMapS3(getBucketTaggingOutput.TagSet), nil
}

func (r ReconcileS3Bucket) reconcileBucketAcl(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	bucket := instance.Spec.Bucket
	desiredAcl := r.bucketAcl(instance)
	currentAcl := instance.Status.Acl
	// this is based on the status and not a value read from AWS
	if desiredAcl != currentAcl {
		logInfof("Changing bucket canned ACL (%s): '%s'", bucket, desiredAcl)

		_, err := r.s3conn.PutBucketAcl(&s3.PutBucketAclInput{
			Bucket: aws.String(bucket),
			ACL:    aws.String(desiredAcl),
		})
		if err != nil {
			if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
				return awsclient.RetryableError(err, "PutBucketAclFailed")
			}
			return awsclient.NonRetryableError(fmt.Errorf("error setting S3 Bucket canned ACL (%s): %s", bucket, err), "PutBucketAclFailed")
		}
		r.sendEvent(instance, apiv1.EventTypeNormal, "PutBucketAcl", "Bucket canned ACL changed to %s", desiredAcl)

		instance.Status.Acl = desiredAcl
		return r.updateInstance(instance)

	}
	return nil
}

func (r ReconcileS3Bucket) reconcileBucketPolicy(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	bucket := instance.Spec.Bucket

	// current policy is already normalized
	currentPolicy, retryError := r.readBucketPolicy(bucket)
	if retryError != nil {
		return retryError
	}
	desiredPolicy := instance.Spec.Policy
	if desiredPolicy != "" {
		var err error
		desiredPolicy, err = structure.NormalizeJsonString(desiredPolicy)
		if err != nil {
			return awsclient.NonRetryableError(fmt.Errorf("error normalizing policy S3 Bucket (%s): %s", bucket, err), "NormalizeJsonBucketPolicyFailed")
		}
	}
	if currentPolicy == desiredPolicy {
		// no update
		return nil
	}
	return r.updateBucketPolicy(instance, currentPolicy)
}

func (r ReconcileS3Bucket) readBucketPolicy(bucket string) (string, *awsclient.RetryError) {
	// read policy
	getBucketPolicyOutput, err := r.s3conn.GetBucketPolicy(&s3.GetBucketPolicyInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
			return "", awsclient.RetryableError(err, "GetBucketPolicyFailed")
		}
		if awsError, ok := err.(awserr.RequestFailure); ok && awsError.StatusCode() == 404 {
			return "", nil
		}
		return "", awsclient.NonRetryableError(fmt.Errorf("error reading S3 Bucket policy (%s): %s", bucket, err), "GetBucketPolicyFailed")
	}
	if getBucketPolicyOutput != nil && getBucketPolicyOutput.Policy != nil {
		policy := *getBucketPolicyOutput.Policy
		if policy != "" {
			policy, err = structure.NormalizeJsonString(policy)
			if err != nil {
				logInfof("[WARN] error normalizing S3 read policy (%s): %s)", bucket, err)
			}
		}
		return policy, nil
	}
	return "", nil
}

func (r ReconcileS3Bucket) updateBucketPolicy(instance *awsv1beta1.S3Bucket, currentPolicy string) *awsclient.RetryError {
	bucket := instance.Spec.Bucket
	desiredPolicy := strings.TrimSpace(instance.Spec.Policy)
	if desiredPolicy != "" {
		logInfof("Updating bucket policy (%s):\n%s", bucket, desiredPolicy)
		_, err := r.s3conn.PutBucketPolicy(&s3.PutBucketPolicyInput{
			Bucket: aws.String(bucket),
			Policy: aws.String(desiredPolicy),
		})
		if err != nil {
			if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
				return awsclient.RetryableError(err, "PutBucketPolicyFailed")
			}
			return awsclient.NonRetryableError(fmt.Errorf("error update S3 Bucket policy (%s): %s", bucket, err), "PutBucketPolicyFailed")
		}
		var reason string
		if currentPolicy == "" {
			reason = "BucketPolicyCreated"
		} else {
			reason = "BucketPolicyUpdated"
		}
		r.sendEvent(instance, apiv1.EventTypeNormal, reason, "Bucket policy changed")
	} else {
		log.Info("Deleting bucket policy", "bucket", bucket)
		_, err := r.s3conn.DeleteBucketPolicy(&s3.DeleteBucketPolicyInput{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
				return awsclient.RetryableError(err, "DeleteBucketPolicyFailed")
			}
			return awsclient.NonRetryableError(fmt.Errorf("error delete S3 Bucket policy (%s): %s", bucket, err), "DeleteBucketPolicyFailed")
		}
		r.sendEvent(instance, apiv1.EventTypeNormal, "BucketPolicyDeleted", "Bucket policy deleted")
	}
	return nil
}

func (r ReconcileS3Bucket) reconcileBucketVersioning(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {

	bucket := instance.Spec.Bucket
	currentVersioning, retryError := r.readBucketVersioning(bucket)
	if retryError != nil {
		return retryError
	}
	var currentEnabled bool
	if currentVersioning != nil {
		currentEnabled = currentVersioning.Enabled
	}

	var desiredEnabled bool
	if instance.Spec.Versioning != nil {
		desiredEnabled = instance.Spec.Versioning.Enabled
	}
	if currentEnabled == desiredEnabled {
		return nil
	}

	log.Info(fmt.Sprintf("Change bucket versioning enabled from %v to %v", currentEnabled, desiredEnabled))

	status := bucketVersioningStatus(desiredEnabled)
	_, err := r.s3conn.PutBucketVersioning(&s3.PutBucketVersioningInput{
		Bucket: aws.String(bucket),
		VersioningConfiguration: &s3.VersioningConfiguration{
			Status: aws.String(status),
		},
	})
	if err != nil {
		if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
			return awsclient.RetryableError(err, "UpdateBucketVersioningFailed")
		}
		return awsclient.NonRetryableError(fmt.Errorf("error update S3 Bucket versioning (%s): %s", bucket, err), "UpdateBucketVersioningFailed")
	}
	r.sendEvent(instance, apiv1.EventTypeNormal, fmt.Sprintf("BucketVersioning%s", status), "Bucket versioning updated")
	return nil
}
func bucketVersioningStatus(enabled bool) string {
	if enabled {
		return s3.BucketVersioningStatusEnabled
	}
	return s3.BucketVersioningStatusSuspended
}

func (r ReconcileS3Bucket) readBucketVersioning(bucket string) (*awsv1beta1.S3BucketVersioning, *awsclient.RetryError) {
	versioning, err := r.s3conn.GetBucketVersioning(&s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
			return nil, awsclient.RetryableError(err, "GetBucketVersioningFailed")
		}
		return nil, awsclient.NonRetryableError(fmt.Errorf("error reading S3 Bucket versioning (%s): %s", bucket, err), "GetBucketVersioningFailed")
	}
	result := &awsv1beta1.S3BucketVersioning{
		Enabled: false,
	}
	if versioning != nil && versioning.Status != nil && *versioning.Status == s3.BucketVersioningStatusEnabled {
		result.Enabled = true
	}
	return result, nil
}

func (r ReconcileS3Bucket) reconcileBucketLifecycle(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	bucket := instance.Spec.Bucket
	currentLifecycle, retryError := r.readBucketLifecycle(bucket)
	if retryError != nil {
		return retryError
	}
	desiredLifecycle := instance.Spec.Lifecycle
	//TODO: implement
	_ = currentLifecycle
	_ = desiredLifecycle
	return nil
}

func (r ReconcileS3Bucket) readBucketLifecycle(bucket string) (*awsv1beta1.S3BucketLifecycle, *awsclient.RetryError) {
	lifecycle, err := r.s3conn.GetBucketLifecycle(&s3.GetBucketLifecycleInput{
		Bucket: aws.String(bucket),
	})

	if err != nil {
		if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
			return nil, awsclient.RetryableError(err, "GetBucketLifecycleFailed")
		}
		if !awsclient.IsAWSCode(err, []string{"NoSuchLifecycleConfiguration"}) {
			return nil, awsclient.NonRetryableError(fmt.Errorf("error getting S3 Bucket Lifecycle configuration (%s): %s", bucket, err), "GetBucketLifecyclFailed")
		}
	}
	if lifecycle == nil {
		return nil, nil
	}
	if lifecycle.Rules == nil || len(lifecycle.Rules) == 0 {
		return nil, nil
	}

	lifecycleConfiguration := awsv1beta1.S3BucketLifecycle{
		LifecycleRules: []awsv1beta1.S3BucketLifecycleRule{},
	}
	for _, ruleObject := range lifecycle.Rules {
		var transition *awsv1beta1.S3BucketLifecycleTransition
		if ruleObject.Transition != nil {
			var date *string
			if ruleObject.Transition.Date != nil {
				date = aws.String((*ruleObject.Transition.Date).Format("2006-01-02"))
			}
			transition = &awsv1beta1.S3BucketLifecycleTransition{
				Days:         ruleObject.Transition.Days,
				Date:         date,
				StorageClass: aws.StringValue(ruleObject.Transition.StorageClass),
			}
		}
		var expiration *awsv1beta1.S3BucketLifecycleExpiration
		if ruleObject.Expiration != nil {
			var date *string
			if ruleObject.Expiration.Date != nil {
				date = aws.String((*ruleObject.Expiration.Date).Format("2006-01-02"))
			}
			expiration = &awsv1beta1.S3BucketLifecycleExpiration{
				Days:                      ruleObject.Expiration.Days,
				Date:                      date,
				ExpiredObjectDeleteMarker: ruleObject.Expiration.ExpiredObjectDeleteMarker,
			}
		}
		var noncurrentVersionTransition *awsv1beta1.S3BucketLifecycleNoncurrentVersionTransition
		if ruleObject.NoncurrentVersionTransition != nil {
			noncurrentVersionTransition = &awsv1beta1.S3BucketLifecycleNoncurrentVersionTransition{
				Days:         aws.Int64Value(ruleObject.NoncurrentVersionTransition.NoncurrentDays),
				StorageClass: aws.StringValue(ruleObject.NoncurrentVersionTransition.StorageClass),
			}
		}
		var noncurrentVersionExpiration *awsv1beta1.S3BucketLifecycleNoncurrentVersionExpiration
		if ruleObject.NoncurrentVersionExpiration != nil {
			noncurrentVersionExpiration = &awsv1beta1.S3BucketLifecycleNoncurrentVersionExpiration{
				Days: aws.Int64Value(ruleObject.NoncurrentVersionExpiration.NoncurrentDays),
			}
		}
		var abortIncompleteMultipartUpload *awsv1beta1.S3BucketLifecycleAbortIncompleteMultipartUpload
		if ruleObject.AbortIncompleteMultipartUpload != nil {
			abortIncompleteMultipartUpload = &awsv1beta1.S3BucketLifecycleAbortIncompleteMultipartUpload{
				DaysAfterInitiation: aws.Int64Value(ruleObject.AbortIncompleteMultipartUpload.DaysAfterInitiation),
			}
		}

		lifecycleRule := awsv1beta1.S3BucketLifecycleRule{
			ID:                             aws.StringValue(ruleObject.ID),
			Prefix:                         aws.StringValue(ruleObject.Prefix),
			Enabled:                        aws.StringValue(ruleObject.Status) == s3.ExpirationStatusEnabled,
			Transition:                     transition,
			Expiration:                     expiration,
			NoncurrentVersionTransition:    noncurrentVersionTransition,
			NoncurrentVersionExpiration:    noncurrentVersionExpiration,
			AbortIncompleteMultipartUpload: abortIncompleteMultipartUpload,
		}
		lifecycleConfiguration.LifecycleRules = append(lifecycleConfiguration.LifecycleRules, lifecycleRule)
	}
	return &lifecycleConfiguration, nil
}

func (r ReconcileS3Bucket) reconcileBucketCORS(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	bucket := instance.Spec.Bucket
	currentCors, retryError := r.readBucketCORSConfiguration(bucket)

	if retryError != nil {
		return retryError
	}
	desiredCors := instance.Spec.CORSConfiguration

	if currentCors == nil && desiredCors == nil {
		return nil
	} else if currentCors == nil && desiredCors != nil {
		logInfof("Creating bucket cors (%s): (%v)", bucket, desiredCors.CORSRules)

		err := r.updateBucketCors(bucket, desiredCors)
		if err != nil {
			if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
				return awsclient.RetryableError(err, "PutBucketCorsFailed")
			}
			return awsclient.NonRetryableError(fmt.Errorf("error create S3 Bucket cors (%s): %s", bucket, err), "PutBucketCorsFailed")
		}
		r.sendEvent(instance, apiv1.EventTypeNormal, "BucketCorsCreated", "Bucket cors created")
	} else if currentCors != nil && desiredCors == nil {
		log.Info("Deleting bucket cors", "bucket", bucket)

		_, err := r.s3conn.DeleteBucketCors(&s3.DeleteBucketCorsInput{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
				return awsclient.RetryableError(err, "DeleteBucketCors")
			}
			return awsclient.NonRetryableError(fmt.Errorf("error delete S3 Bucket cors (%s): %s", bucket, err), "DeleteBucketCorsInputFailed")
		}
		r.sendEvent(instance, apiv1.EventTypeNormal, "BucketCorsDeleted", "Bucket cors deleted")
	} else {
		if !cmp.Equal(currentCors, desiredCors, cmpopts.EquateEmpty()) {
			logInfof("Updating bucket cors (%s): from (%v) to (%v)", bucket, currentCors.CORSRules, desiredCors.CORSRules)
			err := r.updateBucketCors(bucket, desiredCors)
			if err != nil {
				if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
					return awsclient.RetryableError(err, "PutBucketCorsFailed")
				}
				return awsclient.NonRetryableError(fmt.Errorf("error update S3 Bucket cors (%s): %s", bucket, err), "PutBucketCorsFailed")
			}
			r.sendEvent(instance, apiv1.EventTypeNormal, "BucketCorsUpdated", "Bucket cors updated")
		}
	}
	return nil
}

func (r ReconcileS3Bucket) updateBucketCors(bucket string, desiredCors *awsv1beta1.S3BucketCORSConfiguration) error {

	corsRules := make([]*s3.CORSRule, 0)
	for _, desiredRrule := range desiredCors.CORSRules {
		corsRule := &s3.CORSRule{
			AllowedHeaders: aws.StringSlice(desiredRrule.AllowedHeaders),
			AllowedMethods: aws.StringSlice(desiredRrule.AllowedMethods),
			AllowedOrigins: aws.StringSlice(desiredRrule.AllowedOrigins),
			ExposeHeaders:  aws.StringSlice(desiredRrule.ExposeHeaders),
			MaxAgeSeconds:  desiredRrule.MaxAgeSeconds,
		}
		corsRules = append(corsRules, corsRule)
	}
	_, err := r.s3conn.PutBucketCors(&s3.PutBucketCorsInput{
		Bucket: aws.String(bucket),
		CORSConfiguration: &s3.CORSConfiguration{
			CORSRules: corsRules,
		},
	})
	return err

}

func (r ReconcileS3Bucket) readBucketCORSConfiguration(bucket string) (*awsv1beta1.S3BucketCORSConfiguration, *awsclient.RetryError) {
	cors, err := r.s3conn.GetBucketCors(&s3.GetBucketCorsInput{
		Bucket: aws.String(bucket),
	})

	if err != nil {
		if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
			return nil, awsclient.RetryableError(err, "GetBucketCorsFailed")
		}
		if !awsclient.IsAWSCode(err, []string{"NoSuchCORSConfiguration"}) {
			return nil, awsclient.NonRetryableError(fmt.Errorf("error getting S3 Bucket CORS configuration (%s): %s", bucket, err), "GetBucketCorsFailed")
		}
	}
	if cors == nil {
		return nil, nil
	}

	if cors.CORSRules == nil || len(cors.CORSRules) == 0 {
		return nil, nil
	}

	corsConfiguration := awsv1beta1.S3BucketCORSConfiguration{
		CORSRules: []awsv1beta1.S3BucketCORSRule{},
	}
	for _, ruleObject := range cors.CORSRules {
		corsRule := awsv1beta1.S3BucketCORSRule{
			AllowedHeaders: toStringArray(ruleObject.AllowedHeaders),
			AllowedMethods: toStringArray(ruleObject.AllowedMethods),
			AllowedOrigins: toStringArray(ruleObject.AllowedOrigins),
			ExposeHeaders:  toStringArray(ruleObject.ExposeHeaders),
			MaxAgeSeconds:  ruleObject.MaxAgeSeconds,
		}
		corsConfiguration.CORSRules = append(corsConfiguration.CORSRules, corsRule)
	}
	return &corsConfiguration, nil
}

func toStringArray(strings []*string) []string {
	if strings != nil {
		return aws.StringValueSlice(strings)
	} else {
		return []string{}
	}
}

func (r ReconcileS3Bucket) reconcileBucketWebsite(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	bucket := instance.Spec.Bucket
	currentWebsite, retryError := r.readBucketWebsite(bucket)

	if retryError != nil {
		return retryError
	}
	desiredWebsite := instance.Spec.Website
	if currentWebsite == nil && desiredWebsite == nil {
		return nil
	} else if currentWebsite == nil && desiredWebsite != nil {
		logInfof("Creating bucket website (%s): (%v | %v)", bucket, desiredWebsite.Endpoint, desiredWebsite.Redirect)

		err := r.updateBucketWebsite(bucket, desiredWebsite)
		if err != nil {
			if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
				return awsclient.RetryableError(err, "PutBucketWebsiteFailed")
			}
			return awsclient.NonRetryableError(fmt.Errorf("error create S3 Bucket website (%s): %s", bucket, err), "PutBucketWebsiteFailed")
		}
		r.sendEvent(instance, apiv1.EventTypeNormal, "BucketWebsiteCreated", "Bucket website created")
	} else if currentWebsite != nil && desiredWebsite == nil {
		log.Info("Deleting bucket website", "bucket", bucket)

		_, err := r.s3conn.DeleteBucketWebsite(&s3.DeleteBucketWebsiteInput{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
				return awsclient.RetryableError(err, "DeleteBucketWebsite")
			}
			return awsclient.NonRetryableError(fmt.Errorf("error delete S3 Bucket website (%s): %s", bucket, err), "DeleteBucketWebsiteFailed")
		}
		r.sendEvent(instance, apiv1.EventTypeNormal, "BucketWebsiteDeleted", "Bucket website deleted")

	} else {
		if currentWebsite.Endpoint != nil && currentWebsite.Endpoint.RoutingRules != "" {
			normalizedJson, err := structure.NormalizeJsonString(currentWebsite.Endpoint.RoutingRules)
			if err != nil {
				return awsclient.NonRetryableError(fmt.Errorf("error normalizing current website S3 Bucket (%s): %s", bucket, err), "NormalizeJsonBucketWebsiteFailed")
			}
			currentWebsite.Endpoint.RoutingRules = normalizedJson
		}
		if desiredWebsite.Endpoint != nil && desiredWebsite.Endpoint.RoutingRules != "" {
			normalizedJson, err := structure.NormalizeJsonString(desiredWebsite.Endpoint.RoutingRules)
			if err != nil {
				return awsclient.NonRetryableError(fmt.Errorf("error normalizing desired website S3 Bucket (%s): %s", bucket, err), "NormalizeJsonBucketWebsiteFailed")
			}
			desiredWebsite.Endpoint.RoutingRules = normalizedJson
		}
		if !reflect.DeepEqual(currentWebsite, desiredWebsite) {
			logInfof("Updating bucket website (%s): from (%v | %v) to (%v | %v)", bucket, currentWebsite.Endpoint, currentWebsite.Redirect, desiredWebsite.Endpoint, desiredWebsite.Redirect)
			err := r.updateBucketWebsite(bucket, desiredWebsite)
			if err != nil {
				if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
					return awsclient.RetryableError(err, "PutBucketWebsiteFailed")
				}
				return awsclient.NonRetryableError(fmt.Errorf("error update S3 Bucket website (%s): %s", bucket, err), "PutBucketWebsiteFailed")
			}
			r.sendEvent(instance, apiv1.EventTypeNormal, "BucketWebsiteUpdated", "Bucket website updated")
		}
	}
	return nil
}

func (r ReconcileS3Bucket) updateBucketWebsite(bucket string, desiredWebsite *awsv1beta1.S3BucketWebsite) error {
	var indexDocument *s3.IndexDocument
	var errorDocument *s3.ErrorDocument
	var routingRules []*s3.RoutingRule

	if desiredWebsite.Endpoint != nil {
		if desiredWebsite.Endpoint.IndexDocument != "" {
			indexDocument = &s3.IndexDocument{
				Suffix: aws.String(desiredWebsite.Endpoint.IndexDocument),
			}
		}
		if desiredWebsite.Endpoint.ErrorDocument != "" {
			errorDocument = &s3.ErrorDocument{
				Key: aws.String(desiredWebsite.Endpoint.ErrorDocument),
			}
		}
		if desiredWebsite.Endpoint.RoutingRules != "" {
			if err := json.Unmarshal([]byte(desiredWebsite.Endpoint.RoutingRules), &routingRules); err != nil {
				return fmt.Errorf("error while unmarshaling S3 Bucket routing rules (%s): %s", bucket, err)
			}
		}
	}
	var redirectAllRequestsTo *s3.RedirectAllRequestsTo
	if desiredWebsite.Redirect != nil && desiredWebsite.Redirect.HostName != "" {
		var protocol *string
		if desiredWebsite.Redirect.Protocol != "" {
			protocol = aws.String(desiredWebsite.Redirect.Protocol)
		}
		redirectAllRequestsTo = &s3.RedirectAllRequestsTo{
			HostName: aws.String(desiredWebsite.Redirect.HostName),
			Protocol: protocol,
		}
	}

	_, err := r.s3conn.PutBucketWebsite(&s3.PutBucketWebsiteInput{
		Bucket: aws.String(bucket),
		WebsiteConfiguration: &s3.WebsiteConfiguration{
			IndexDocument:         indexDocument,
			ErrorDocument:         errorDocument,
			RoutingRules:          routingRules,
			RedirectAllRequestsTo: redirectAllRequestsTo,
		},
	})
	return err
}

func (r ReconcileS3Bucket) readBucketWebsite(bucket string) (*awsv1beta1.S3BucketWebsite, *awsclient.RetryError) {
	website, err := r.s3conn.GetBucketWebsite(&s3.GetBucketWebsiteInput{
		Bucket: aws.String(bucket),
	})

	if err != nil {
		if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
			return nil, awsclient.RetryableError(err, "GetBucketWebsiteFailed")
		}
		if !awsclient.IsAWSCode(err, []string{"NoSuchWebsiteConfiguration", "NotImplemented"}) {
			return nil, awsclient.NonRetryableError(fmt.Errorf("error reading S3 Bucket website (%s): %s", bucket, err), "GetBucketWebsiteFailed")
		}
	}
	if website == nil {
		return nil, nil
	}
	if website.IndexDocument != nil && website.IndexDocument.Suffix != nil {
		var errorDocument string
		if website.ErrorDocument != nil {
			errorDocument = aws.StringValue(website.ErrorDocument.Key)
		}
		var routingRules string
		if website.RoutingRules != nil {
			routingRules, err = normalizeRoutingRules(website.RoutingRules)
			if err != nil {
				return nil, awsclient.NonRetryableError(fmt.Errorf("error while marshaling S3 Bucket routing rules (%s): %s", bucket, err), "NormalizeRoutingRulesFailed")
			}
		}

		return &awsv1beta1.S3BucketWebsite{
			Endpoint: &awsv1beta1.S3BucketWebsiteEndpoint{
				IndexDocument: aws.StringValue(website.IndexDocument.Suffix),
				ErrorDocument: errorDocument,
				RoutingRules:  routingRules,
			},
		}, nil
	} else if website.RedirectAllRequestsTo != nil && website.RedirectAllRequestsTo.HostName != nil {
		return &awsv1beta1.S3BucketWebsite{
			Redirect: &awsv1beta1.S3BucketWebsiteRedirect{
				HostName: aws.StringValue(website.RedirectAllRequestsTo.HostName),
				Protocol: aws.StringValue(website.RedirectAllRequestsTo.Protocol),
			},
		}, nil
	}
	return nil, nil
}

func normalizeRoutingRules(w []*s3.RoutingRule) (string, error) {
	withNulls, err := json.Marshal(w)
	if err != nil {
		return "", err
	}

	var rules []map[string]interface{}
	if err := json.Unmarshal(withNulls, &rules); err != nil {
		return "", err
	}

	var cleanRules []map[string]interface{}
	for _, rule := range rules {
		cleanRules = append(cleanRules, removeNil(rule))
	}

	withoutNulls, err := json.Marshal(cleanRules)
	if err != nil {
		return "", err
	}

	return string(withoutNulls), nil
}

func removeNil(data map[string]interface{}) map[string]interface{} {
	withoutNil := make(map[string]interface{})

	for k, v := range data {
		if v == nil {
			continue
		}

		switch v.(type) {
		case map[string]interface{}:
			withoutNil[k] = removeNil(v.(map[string]interface{}))
		default:
			withoutNil[k] = v
		}
	}

	return withoutNil
}

func (r ReconcileS3Bucket) reconcileBucketLogging(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	bucket := instance.Spec.Bucket
	currentLogging, retryError := r.readBucketLogging(bucket)
	if retryError != nil {
		return retryError
	}
	desiredLogging := instance.Spec.Logging
	if currentLogging == nil && desiredLogging == nil {
		return nil
	} else if currentLogging == nil && desiredLogging != nil {
		log.Info(fmt.Sprintf("Creating bucket logging (%s,%s)", desiredLogging.TargetBucket, desiredLogging.TargetPrefix), "bucket", bucket)
		err := r.updateBucketLogging(bucket, *desiredLogging)
		if err != nil {
			if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
				return awsclient.RetryableError(err, "CreateBucketLoggingFailed")
			}
			return awsclient.NonRetryableError(fmt.Errorf("error create S3 Bucket logging (%s): %s", bucket, err), "CreateBucketLoggingFailed")
		}
		r.sendEvent(instance, apiv1.EventTypeNormal, "BucketLoggingCreated", "Bucket logging created")
	} else if currentLogging != nil && desiredLogging == nil {
		log.Info(fmt.Sprintf("Deleting bucket logging (%s,%s)", currentLogging.TargetBucket, currentLogging.TargetPrefix), "bucket", bucket)
		_, err := r.s3conn.PutBucketLogging(&s3.PutBucketLoggingInput{
			Bucket:              aws.String(bucket),
			BucketLoggingStatus: &s3.BucketLoggingStatus{},
		})
		if err != nil {
			if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
				return awsclient.RetryableError(err, "DeleteBucketLoggingFailed")
			}
			return awsclient.NonRetryableError(fmt.Errorf("error delete S3 Bucket logging (%s): %s", bucket, err), "DeleteBucketLoggingFailed")
		}
		r.sendEvent(instance, apiv1.EventTypeNormal, "BucketLoggingDeleted", "Bucket logging deleted")
	} else if !reflect.DeepEqual(currentLogging, desiredLogging) {
		log.Info(fmt.Sprintf("Updating bucket logging from (%s,%s) to (%s,%s)", currentLogging.TargetBucket, currentLogging.TargetPrefix, desiredLogging.TargetBucket, desiredLogging.TargetPrefix), "bucket", bucket)

		err := r.updateBucketLogging(bucket, *desiredLogging)
		if err != nil {
			if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
				return awsclient.RetryableError(err, "UpdateBucketLoggingFailed")
			}
			return awsclient.NonRetryableError(fmt.Errorf("error update S3 Bucket logging (%s): %s", bucket, err), "UpdateBucketLoggingFailed")
		}
		r.sendEvent(instance, apiv1.EventTypeNormal, "BucketLoggingUpdated", "Bucket logging updated")
	}
	return nil
}

func (r ReconcileS3Bucket) updateBucketLogging(bucket string, desiredLogging awsv1beta1.S3BucketLogging) error {
	_, err := r.s3conn.PutBucketLogging(&s3.PutBucketLoggingInput{
		Bucket: aws.String(bucket),
		BucketLoggingStatus: &s3.BucketLoggingStatus{
			LoggingEnabled: &s3.LoggingEnabled{
				TargetBucket: aws.String(desiredLogging.TargetBucket),
				TargetPrefix: aws.String(desiredLogging.TargetPrefix),
			},
		},
	})
	return err
}

func (r ReconcileS3Bucket) readBucketLogging(bucket string) (*awsv1beta1.S3BucketLogging, *awsclient.RetryError) {
	logging, err := r.s3conn.GetBucketLogging(&s3.GetBucketLoggingInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
			return nil, awsclient.RetryableError(err, "GetBucketLoggingFailed")
		}
		return nil, awsclient.NonRetryableError(fmt.Errorf("error reading S3 Bucket logging (%s): %s", bucket, err), "GetBucketLoggingFailed")
	}
	if logging.LoggingEnabled != nil && logging.LoggingEnabled.TargetBucket != nil {
		return &awsv1beta1.S3BucketLogging{
			TargetBucket: aws.StringValue(logging.LoggingEnabled.TargetBucket),
			TargetPrefix: aws.StringValue(logging.LoggingEnabled.TargetPrefix),
		}, nil
	}
	return nil, nil
}

func (r ReconcileS3Bucket) reconcileBucketEncryption(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	bucket := instance.Spec.Bucket
	currentEncryption, retryError := r.readBucketEncryption(bucket)
	if retryError != nil {
		return retryError
	}
	var desiredEncryption *awsv1beta1.S3ServerSideEncryptionByDefault
	if instance.Spec.ServerSideEncryptionConfiguration != nil {
		desiredEncryption = &instance.Spec.ServerSideEncryptionConfiguration.Rule.ApplyServerSideEncryptionByDefault
	}
	if currentEncryption == nil && desiredEncryption == nil {
		return nil
	} else if currentEncryption == nil && desiredEncryption != nil {
		log.Info(fmt.Sprintf("Creating bucket encryption (%s,%s)", desiredEncryption.SSEAlgorithm, desiredEncryption.KMSMasterKeyID), "bucket", bucket)
		err := r.updateBucketEncryption(bucket, *desiredEncryption)
		if err != nil {
			if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
				return awsclient.RetryableError(err, "CreateBucketEncryptionFailed")
			}
			return awsclient.NonRetryableError(fmt.Errorf("error create S3 Bucket encryption (%s): %s", bucket, err), "CreateBucketEncryptionFailed")
		}
		r.sendEvent(instance, apiv1.EventTypeNormal, "BucketEncryptionCreated", "Bucket encryption created")
	} else if currentEncryption != nil && desiredEncryption == nil {
		log.Info(fmt.Sprintf("Deleting bucket encryption (%s,%s)", currentEncryption.SSEAlgorithm, currentEncryption.KMSMasterKeyID), "bucket", bucket)
		_, err := r.s3conn.DeleteBucketEncryption(&s3.DeleteBucketEncryptionInput{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
				return awsclient.RetryableError(err, "DeleteBucketEncryptionFailed")
			}
			return awsclient.NonRetryableError(fmt.Errorf("error delete S3 Bucket encryption (%s): %s", bucket, err), "DeleteBucketEncryptionFailed")
		}
		r.sendEvent(instance, apiv1.EventTypeNormal, "BucketEncryptionDeleted", "Bucket policy deleted")
	} else if !reflect.DeepEqual(currentEncryption, desiredEncryption) {
		log.Info(fmt.Sprintf("Updating bucket encryption from (%s,%s) to (%s,%s)", currentEncryption.SSEAlgorithm, currentEncryption.KMSMasterKeyID, desiredEncryption.SSEAlgorithm, desiredEncryption.KMSMasterKeyID), "bucket", bucket)
		err := r.updateBucketEncryption(bucket, *desiredEncryption)
		if err != nil {
			if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
				return awsclient.RetryableError(err, "UpdateBucketEncryptionFailed")
			}
			return awsclient.NonRetryableError(fmt.Errorf("error update S3 Bucket encryption (%s): %s", bucket, err), "UpdateBucketEncryptionFailed")
		}
		r.sendEvent(instance, apiv1.EventTypeNormal, "BucketEncryptionUpdated", "Bucket encryption updated")
	}
	return nil
}

func (r ReconcileS3Bucket) updateBucketEncryption(bucket string, desiredEncryption awsv1beta1.S3ServerSideEncryptionByDefault) error {
	var kmsMasterKeyID *string
	if desiredEncryption.KMSMasterKeyID != "" {
		kmsMasterKeyID = aws.String(desiredEncryption.KMSMasterKeyID)
	}

	_, err := r.s3conn.PutBucketEncryption(&s3.PutBucketEncryptionInput{
		Bucket: aws.String(bucket),
		ServerSideEncryptionConfiguration: &s3.ServerSideEncryptionConfiguration{
			Rules: []*s3.ServerSideEncryptionRule{
				{
					ApplyServerSideEncryptionByDefault: &s3.ServerSideEncryptionByDefault{
						SSEAlgorithm:   aws.String(desiredEncryption.SSEAlgorithm),
						KMSMasterKeyID: kmsMasterKeyID,
					},
				},
			},
		},
	})
	return err
}

func (r ReconcileS3Bucket) readBucketEncryption(bucket string) (*awsv1beta1.S3ServerSideEncryptionByDefault, *awsclient.RetryError) {
	// read bucket encryption
	encryption, err := r.s3conn.GetBucketEncryption(&s3.GetBucketEncryptionInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
			return nil, awsclient.RetryableError(err, "GetBucketEncryptionFailed")
		}
		if !awsclient.IsAWSCode(err, []string{"ServerSideEncryptionConfigurationNotFoundError"}) {
			return nil, awsclient.NonRetryableError(fmt.Errorf("error reading S3 Bucket encryption (%s): %s", bucket, err), "GetBucketEncryptionFailed")
		}
	}
	if encryption != nil && encryption.ServerSideEncryptionConfiguration != nil {
		for _, v := range encryption.ServerSideEncryptionConfiguration.Rules {
			if v.ApplyServerSideEncryptionByDefault != nil && v.ApplyServerSideEncryptionByDefault.SSEAlgorithm != nil {
				return &awsv1beta1.S3ServerSideEncryptionByDefault{
					KMSMasterKeyID: aws.StringValue(v.ApplyServerSideEncryptionByDefault.KMSMasterKeyID),
					SSEAlgorithm:   aws.StringValue(v.ApplyServerSideEncryptionByDefault.SSEAlgorithm),
				}, nil
			}
		}
	}
	return nil, nil
}

func (r ReconcileS3Bucket) createBucket(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {

	bucket := instance.Spec.Bucket
	bucketArn := r.bucketARN(instance)
	region := r.bucketRegion(instance)
	bucketAcl := r.bucketAcl(instance)

	// create bucket
	log.Info("Creating S3 bucket", "bucket", bucket, "region", region)
	_, err := r.s3conn.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(bucket),
		ACL:    aws.String(bucketAcl),
		CreateBucketConfiguration: &s3.CreateBucketConfiguration{
			LocationConstraint: aws.String(region),
		},
	})
	if err != nil {
		if awsclient.IsAWSCode(err, []string{"OperationAborted"}) {
			log.Error(err, "[WARN] Got an error while trying to create S3 bucket", "bucket", bucket)
			return awsclient.RetryableError(fmt.Errorf("error creating S3 Bucket (%s): %s", bucket, err), "CreateBucketFailed")
		}
		return awsclient.NonRetryableError(err, "CreateBucketFailed")
	}
	r.sendEvent(instance, apiv1.EventTypeNormal, "BucketCreated", "Successfully created S3 bucket %s", bucketArn)

	// update status
	log.Info("S3 bucket created", "bucket", bucket)

	instance.Status.LocationConstraint = region
	instance.Status.ARN = bucketArn
	instance.Status.Acl = bucketAcl

	if retryError := r.updateInstance(instance); retryError != nil {
		return retryError
	}
	return nil
}

func (r ReconcileS3Bucket) sendEvent(instance *awsv1beta1.S3Bucket, eventType, reason, messageFmt string, args ...interface{}) {
	r.recorder.Eventf(instance, eventType, reason, messageFmt, args...)
}

func (r ReconcileS3Bucket) updateInstance(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	if err := r.Update(context.Background(), instance); err != nil {
		log.Error(err, "[ERROR] Got an error while updating S3Bucket status", "bucket", instance.Spec.Bucket)
		return awsclient.NonRetryableError(err, "UpdateInstanceFailed")
	}
	return nil
}

func (r ReconcileS3Bucket) deleteBucket(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	if instance.Spec.DeleteStrategy == awsv1beta1.SkipDeleteStrategy {
		log.Info("Skip S3 bucket deletion. Delete strategy is Skip")
		return nil
	}
	bucketArn := instance.Status.ARN
	if bucketArn == "" {
		log.Info("Skip S3 bucket deletion. Status.ARN is empty", "bucket", instance.Spec.Bucket)
		return nil
	}
	parsedArn, err := arn.Parse(bucketArn)
	if err != nil {
		log.Error(err, "Skip S3 bucket deletion. Status.ARN is invalid", "bucket", instance.Spec.Bucket)
		return nil
	}
	bucket := parsedArn.Resource
	log.Info("Deleting S3 bucket", "bucket", bucket, "arn", instance.Status.ARN, "deleteStrategy", instance.Spec.DeleteStrategy)

	_, err = r.s3conn.DeleteBucket(&s3.DeleteBucketInput{
		Bucket: aws.String(bucket),
	})

	if err != nil {
		if awsclient.IsAWSErr(err, s3.ErrCodeNoSuchBucket, "") {
			log.Info("[WARN] S3 Bucket to delete not found", "bucket", bucket)
			return nil
		}
		if awsclient.IsAWSErr(err, "BucketNotEmpty", "") && instance.Spec.DeleteStrategy == awsv1beta1.ForceDeleteStrategy {
			retryError := r.deleteBucketObjects(bucket)
			if retryError != nil {
				return retryError
			}
			log.Info("Deleting S3 bucket after bucket objects deletion", "bucket", bucket)
			_, err = r.s3conn.DeleteBucket(&s3.DeleteBucketInput{
				Bucket: aws.String(bucket),
			})
			if err != nil {
				return awsclient.NonRetryableError(err, "DeleteBucketFailed")
			}
		} else {
			return awsclient.NonRetryableError(err, "DeleteBucketFailed")
		}
	}
	log.Info("S3 bucket deleted", "bucket", bucket)
	return nil
}

func (r ReconcileS3Bucket) deleteBucketObjects(bucket string) *awsclient.RetryError {
	log.Info("Deleting S3 bucket objects", "bucket", bucket)

	iter := s3manager.NewDeleteListIterator(r.s3conn, &s3.ListObjectsInput{
		Bucket: aws.String(bucket),
	})
	err := s3manager.NewBatchDeleteWithClient(r.s3conn).Delete(aws.BackgroundContext(), iter)
	if err != nil {
		return awsclient.NonRetryableError(err, "BatchDeleteWithClientFailed")
	}
	log.Info("S3 bucket objects deleted", "bucket", bucket)
	return nil
}

func logInfof(format string, a ...interface{}) {
	log.Info(fmt.Sprintf(format, a...))
}
