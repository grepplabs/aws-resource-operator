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
	"fmt"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
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
		awsClient: awsclient.GetAWSClient(),
		recorder:  mgr.GetRecorder("s3bucket-controller"),
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
	scheme    *runtime.Scheme
	awsClient *awsclient.AWSClient
	recorder  record.EventRecorder
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
	if err != nil {
		return r.handleRetryError(instance, retryError)
	}

	retryError = r.reconcileInstance(request, instance)
	if retryError != nil {
		return r.handleRetryError(instance, retryError)
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileS3Bucket) handleRetryError(instance *awsv1beta1.S3Bucket, retryError *awsclient.RetryError) (reconcile.Result, error) {
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

func (r *ReconcileS3Bucket) validateInstance(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	bucket := instance.Spec.Bucket

	if bucket == "" {
		return awsclient.NonRetryableError(fmt.Errorf("bucket name is empty"), "BucketNameEmpty")
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

func (r *ReconcileS3Bucket) bucketARN(instance *awsv1beta1.S3Bucket) string {
	return arn.ARN{
		Partition: r.awsClient.Partition(),
		Service:   "s3",
		Resource:  instance.Spec.Bucket,
	}.String()
}

func (r *ReconcileS3Bucket) bucketRegion(instance *awsv1beta1.S3Bucket) string {
	if instance.Spec.Region != "" {
		return instance.Spec.Region
	}
	return r.awsClient.Region()
}
func (r *ReconcileS3Bucket) bucketAcl(instance *awsv1beta1.S3Bucket) string {
	if instance.Spec.Acl != "" {
		return instance.Spec.Acl
	}
	return "private"
}

func (r *ReconcileS3Bucket) reconcileInstance(request reconcile.Request, instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
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
		if strings.EqualFold(string(instance.Spec.OwnershipStrategy), string(awsv1beta1.AcquireOwnershipStrategy)) {
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

func (r *ReconcileS3Bucket) acquireBucketOwnership(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	s3conn := r.awsClient.S3()
	bucket := instance.Spec.Bucket
	bucketArn := r.bucketARN(instance)
	locationConstraint := r.bucketRegion(instance)

	log.Info("Acquiring S3 bucket ownership", "bucket", bucket, "locationConstraint", locationConstraint)

	// get bucket location
	getBucketLocationOutput, err := s3conn.GetBucketLocation(&s3.GetBucketLocationInput{
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
		if locationConstraint != *getBucketLocationOutput.LocationConstraint {
			errorString := fmt.Sprintf("location constraint configuration of S3Bucket changed from %s to %s", *getBucketLocationOutput.LocationConstraint, locationConstraint)
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

func (r *ReconcileS3Bucket) checkBucketExists(instance *awsv1beta1.S3Bucket) (bool, *awsclient.RetryError) {
	bucket := instance.Spec.Bucket
	// check bucket exists
	_, err := r.awsClient.S3().HeadBucket(&s3.HeadBucketInput{
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

func (r *ReconcileS3Bucket) reconcileBucket(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	if err := r.reconcileBucketPolicy(instance); err != nil {
		return err
	}
	if err := r.reconcileBucketAcl(instance); err != nil {
		return err
	}
	return nil
}

func (r *ReconcileS3Bucket) reconcileBucketAcl(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	bucket := instance.Spec.Bucket
	desiredAcl := r.bucketAcl(instance)
	currentAcl := instance.Status.Acl
	// this is based on the status and not a value read from AWS
	if desiredAcl != currentAcl {
		logInfof("Changing bucket canned ACL (%s): '%s'", bucket, desiredAcl)

		_, err := r.awsClient.S3().PutBucketAcl(&s3.PutBucketAclInput{
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

func (r *ReconcileS3Bucket) reconcileBucketPolicy(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	bucket := instance.Spec.Bucket

	// current policy is already normalized
	currentPolicy, retryError := r.readyBucketPolicy(bucket)
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
	return r.updateBucketPolicy(instance)
}

func (r *ReconcileS3Bucket) readyBucketPolicy(bucket string) (string, *awsclient.RetryError) {
	// read policy
	getBucketPolicyInputOutput, err := r.awsClient.S3().GetBucketPolicy(&s3.GetBucketPolicyInput{
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
	if getBucketPolicyInputOutput != nil && getBucketPolicyInputOutput.Policy != nil {
		policy := *getBucketPolicyInputOutput.Policy
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

func (r *ReconcileS3Bucket) updateBucketPolicy(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	bucket := instance.Spec.Bucket
	desiredPolicy := strings.TrimSpace(instance.Spec.Policy)
	if desiredPolicy != "" {
		logInfof("Updating bucket policy (%s):\n%s", bucket, desiredPolicy)
		_, err := r.awsClient.S3().PutBucketPolicy(&s3.PutBucketPolicyInput{
			Bucket: aws.String(bucket),
			Policy: aws.String(desiredPolicy),
		})
		if err != nil {
			if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
				return awsclient.RetryableError(err, "PutBucketPolicyFailed")
			}
			return awsclient.NonRetryableError(fmt.Errorf("error update S3 Bucket policy (%s): %s", bucket, err), "PutBucketPolicyFailed")
		}
		r.sendEvent(instance, apiv1.EventTypeNormal, "PutBucketPolicy", "Bucket policy changed")
	} else {
		log.Info("Deleting bucket policy", "bucket", bucket)
		_, err := r.awsClient.S3().DeleteBucketPolicy(&s3.DeleteBucketPolicyInput{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
				return awsclient.RetryableError(err, "DeleteBucketPolicyFailed")
			}
			return awsclient.NonRetryableError(fmt.Errorf("error delete S3 Bucket policy (%s): %s", bucket, err), "DeleteBucketPolicyFailed")
		}
		r.sendEvent(instance, apiv1.EventTypeNormal, "DeleteBucketPolicy", "Bucket policy deleted")
	}
	return nil
}

func (r *ReconcileS3Bucket) createBucket(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	s3conn := r.awsClient.S3()

	bucket := instance.Spec.Bucket
	bucketArn := r.bucketARN(instance)
	locationConstraint := r.bucketRegion(instance)
	bucketAcl := r.bucketAcl(instance)

	// create bucket
	log.Info("Creating S3 bucket", "bucket", bucket, "locationConstraint", locationConstraint)
	_, err := s3conn.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(bucket),
		ACL:    aws.String(bucketAcl),
		CreateBucketConfiguration: &s3.CreateBucketConfiguration{
			LocationConstraint: aws.String(locationConstraint),
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

	instance.Status.LocationConstraint = r.bucketRegion(instance)
	instance.Status.ARN = bucketArn
	instance.Status.Acl = bucketAcl

	if retryError := r.updateInstance(instance); retryError != nil {
		return retryError
	}
	return nil
}

func (r *ReconcileS3Bucket) sendEvent(instance *awsv1beta1.S3Bucket, eventType, reason, messageFmt string, args ...interface{}) {
	r.recorder.Eventf(instance, eventType, reason, messageFmt, args...)
}

func (r *ReconcileS3Bucket) updateInstance(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	if err := r.Update(context.Background(), instance); err != nil {
		log.Error(err, "[ERROR] Got an error while updating S3Bucket status", "bucket", instance.Spec.Bucket)
		return awsclient.NonRetryableError(err, "UpdateInstanceFailed")
	}
	return nil
}

func (r *ReconcileS3Bucket) deleteBucket(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	if strings.EqualFold(string(instance.Spec.DeleteStrategy), string(awsv1beta1.SkipDeleteStrategy)) {
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

	_, err = r.awsClient.S3().DeleteBucket(&s3.DeleteBucketInput{
		Bucket: aws.String(bucket),
	})

	if err != nil {
		if awsclient.IsAWSErr(err, s3.ErrCodeNoSuchBucket, "") {
			log.Info("[WARN] S3 Bucket to delete not found", "bucket", bucket)
			return nil
		}
		if awsclient.IsAWSErr(err, "BucketNotEmpty", "") && strings.EqualFold(string(instance.Spec.DeleteStrategy), string(awsv1beta1.ForceDeleteStrategy)) {
			retryError := r.deleteBucketObjects(bucket)
			if retryError != nil {
				return retryError
			}
			log.Info("Deleting S3 bucket after bucket objects deletion", "bucket", bucket)
			_, err = r.awsClient.S3().DeleteBucket(&s3.DeleteBucketInput{
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

func (r *ReconcileS3Bucket) deleteBucketObjects(bucket string) *awsclient.RetryError {
	log.Info("Deleting S3 bucket objects", "bucket", bucket)

	iter := s3manager.NewDeleteListIterator(r.awsClient.S3(), &s3.ListObjectsInput{
		Bucket: aws.String(bucket),
	})
	err := s3manager.NewBatchDeleteWithClient(r.awsClient.S3()).Delete(aws.BackgroundContext(), iter)
	if err != nil {
		return awsclient.NonRetryableError(err, "BatchDeleteWithClientFailed")
	}
	log.Info("S3 bucket objects deleted", "bucket", bucket)
	return nil
}

func logInfof(format string, a ...interface{}) {
	log.Info(fmt.Sprintf(format, a...))
}
