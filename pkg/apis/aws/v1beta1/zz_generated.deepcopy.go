// +build !ignore_autogenerated

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
// Code generated by main. DO NOT EDIT.

package v1beta1

import (
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *S3Bucket) DeepCopyInto(out *S3Bucket) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new S3Bucket.
func (in *S3Bucket) DeepCopy() *S3Bucket {
	if in == nil {
		return nil
	}
	out := new(S3Bucket)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *S3Bucket) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *S3BucketList) DeepCopyInto(out *S3BucketList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]S3Bucket, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new S3BucketList.
func (in *S3BucketList) DeepCopy() *S3BucketList {
	if in == nil {
		return nil
	}
	out := new(S3BucketList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *S3BucketList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *S3BucketLogging) DeepCopyInto(out *S3BucketLogging) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new S3BucketLogging.
func (in *S3BucketLogging) DeepCopy() *S3BucketLogging {
	if in == nil {
		return nil
	}
	out := new(S3BucketLogging)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *S3BucketServerSideEncryptionConfiguration) DeepCopyInto(out *S3BucketServerSideEncryptionConfiguration) {
	*out = *in
	out.Rule = in.Rule
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new S3BucketServerSideEncryptionConfiguration.
func (in *S3BucketServerSideEncryptionConfiguration) DeepCopy() *S3BucketServerSideEncryptionConfiguration {
	if in == nil {
		return nil
	}
	out := new(S3BucketServerSideEncryptionConfiguration)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *S3BucketSpec) DeepCopyInto(out *S3BucketSpec) {
	*out = *in
	if in.Tags != nil {
		in, out := &in.Tags, &out.Tags
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.ServerSideEncryptionConfiguration != nil {
		in, out := &in.ServerSideEncryptionConfiguration, &out.ServerSideEncryptionConfiguration
		*out = new(S3BucketServerSideEncryptionConfiguration)
		**out = **in
	}
	if in.Versioning != nil {
		in, out := &in.Versioning, &out.Versioning
		*out = new(S3BucketVersioning)
		**out = **in
	}
	if in.Logging != nil {
		in, out := &in.Logging, &out.Logging
		*out = new(S3BucketLogging)
		**out = **in
	}
	if in.Website != nil {
		in, out := &in.Website, &out.Website
		*out = new(S3BucketWebsite)
		(*in).DeepCopyInto(*out)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new S3BucketSpec.
func (in *S3BucketSpec) DeepCopy() *S3BucketSpec {
	if in == nil {
		return nil
	}
	out := new(S3BucketSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *S3BucketStatus) DeepCopyInto(out *S3BucketStatus) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new S3BucketStatus.
func (in *S3BucketStatus) DeepCopy() *S3BucketStatus {
	if in == nil {
		return nil
	}
	out := new(S3BucketStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *S3BucketVersioning) DeepCopyInto(out *S3BucketVersioning) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new S3BucketVersioning.
func (in *S3BucketVersioning) DeepCopy() *S3BucketVersioning {
	if in == nil {
		return nil
	}
	out := new(S3BucketVersioning)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *S3BucketWebsite) DeepCopyInto(out *S3BucketWebsite) {
	*out = *in
	if in.Endpoint != nil {
		in, out := &in.Endpoint, &out.Endpoint
		*out = new(S3BucketWebsiteEndpoint)
		**out = **in
	}
	if in.Redirect != nil {
		in, out := &in.Redirect, &out.Redirect
		*out = new(S3BucketWebsiteRedirect)
		**out = **in
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new S3BucketWebsite.
func (in *S3BucketWebsite) DeepCopy() *S3BucketWebsite {
	if in == nil {
		return nil
	}
	out := new(S3BucketWebsite)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *S3BucketWebsiteEndpoint) DeepCopyInto(out *S3BucketWebsiteEndpoint) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new S3BucketWebsiteEndpoint.
func (in *S3BucketWebsiteEndpoint) DeepCopy() *S3BucketWebsiteEndpoint {
	if in == nil {
		return nil
	}
	out := new(S3BucketWebsiteEndpoint)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *S3BucketWebsiteRedirect) DeepCopyInto(out *S3BucketWebsiteRedirect) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new S3BucketWebsiteRedirect.
func (in *S3BucketWebsiteRedirect) DeepCopy() *S3BucketWebsiteRedirect {
	if in == nil {
		return nil
	}
	out := new(S3BucketWebsiteRedirect)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *S3ServerSideEncryptionByDefault) DeepCopyInto(out *S3ServerSideEncryptionByDefault) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new S3ServerSideEncryptionByDefault.
func (in *S3ServerSideEncryptionByDefault) DeepCopy() *S3ServerSideEncryptionByDefault {
	if in == nil {
		return nil
	}
	out := new(S3ServerSideEncryptionByDefault)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *S3ServerSideEncryptionRule) DeepCopyInto(out *S3ServerSideEncryptionRule) {
	*out = *in
	out.ApplyServerSideEncryptionByDefault = in.ApplyServerSideEncryptionByDefault
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new S3ServerSideEncryptionRule.
func (in *S3ServerSideEncryptionRule) DeepCopy() *S3ServerSideEncryptionRule {
	if in == nil {
		return nil
	}
	out := new(S3ServerSideEncryptionRule)
	in.DeepCopyInto(out)
	return out
}
