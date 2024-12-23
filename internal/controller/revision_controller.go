/*
Copyright 2024.

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

package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appsv1alpha1 "github.com/LyridInc/lyrid-operator/api/v1alpha1"
	"github.com/LyridInc/lyrid-operator/pkg/lyra"
)

// RevisionReconciler reconciles a Revision object
type RevisionReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=apps.lyrid.io,resources=revisions,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps.lyrid.io,resources=revisions/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps.lyrid.io,resources=revisions/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Revision object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *RevisionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	accountSecret := &corev1.Secret{}
	if err := r.Client.Get(context.Background(), types.NamespacedName{Name: "lyrid.secretkey", Namespace: req.Namespace}, accountSecret); err != nil {
		if errors.IsNotFound(err) {
			fmt.Printf("lyrid.secretkey secret is not found in namespace %s\n", req.Namespace)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	revision := &appsv1alpha1.Revision{}
	if err := r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, revision); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Revision is deleted ->")
			revision.Name = req.Name
			revision.Namespace = req.Namespace
			lyraClient := lyra.NewLyraClient(r.Client, revision.Namespace)
			deleteRevisionResponse, err := lyraClient.DeleteRevision(*revision, string(accountSecret.Data["key"]), string(accountSecret.Data["secret"]))
			if err != nil {
				fmt.Println("Response:", deleteRevisionResponse)
				fmt.Println("Error:", err)
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	fmt.Printf("------------------------------------- RECONCILE REVISION: %s  -------------------------------------\n", revision.Spec.Id)
	defer func() {
		fmt.Printf("------------------------------------- RECONCILE REVISION: %s FINISHED -------------------------------------\n", revision.Spec.Id)
	}()

	if revision.Generation != revision.Status.ObservedGeneration {
		defer func() {
			revision.Status.ObservedGeneration = revision.Generation
			if err := r.Client.Status().Update(ctx, revision); err != nil {
				fmt.Println("Update generation error:", err)
			}
		}()
	} else {
		return ctrl.Result{}, nil
	}

	if origin, ok := revision.Annotations["origin"]; ok && origin == "app-deployment" {
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			// Fetch the latest resource
			if err := r.Get(ctx, req.NamespacedName, revision); err != nil {
				return err
			}

			// Update the annotation
			if revision.Annotations == nil {
				revision.Annotations = make(map[string]string)
			}
			revision.Annotations["origin"] = ""

			// Attempt to update
			return r.Client.Update(ctx, revision)
		})

		if err != nil {
			log.Error(err, "Failed to update Revision instance after retries")
			return ctrl.Result{}, err
		}
	}

	fmt.Println("REVISION:", revision.Name, revision.Status.Phase)
	// this assume we switch up the active revision
	if revision.Spec.Status == "Active" {
		revisionHandleAppDeploymentChanges(ctx, r, *revision)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RevisionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &appsv1alpha1.Revision{}, ".metadata.ownerReferences", func(rawObj client.Object) []string {
		// Grab the Revision object, then extract its owner references
		revision := rawObj.(*appsv1alpha1.Revision)
		ownerRefs := revision.GetOwnerReferences()

		// Collect and return the UIDs of all owners
		ownerUIDs := make([]string, len(ownerRefs))
		for i, owner := range ownerRefs {
			ownerUIDs[i] = string(owner.UID)
		}
		return ownerUIDs
	}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1alpha1.Revision{}).
		Complete(r)
}

func revisionHandleAppDeploymentChanges(ctx context.Context, r *RevisionReconciler, revision appsv1alpha1.Revision) (bool, bool, error) {
	var createNewApp bool
	var createNewRevision bool
	found := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: revision.Spec.Ref.AppDeployment["name"], Namespace: revision.Namespace}, found); err == nil {

		if revision.Spec.Status == "Active" {
			appDeploy := &appsv1alpha1.AppDeployment{}
			if err := r.Get(ctx, types.NamespacedName{Name: revision.Spec.Ref.AppDeployment["name"], Namespace: revision.Namespace}, appDeploy); err != nil {
				return false, false, err
			}
			defer func() {
				appDeploy.Annotations["changes"] = "revision-reconciler"
				appDeploy.Spec.CurrentRevisionId = revision.Spec.Id
				fmt.Println("Revision: Update AppDeployment -> Set app deploy revision id to:", revision.Spec.Id)
				if err := r.Client.Update(ctx, appDeploy); err != nil {
					fmt.Println("Error set app deploy revision id:", err)
				}
			}()

			revisionsList := appsv1alpha1.RevisionList{}
			if err := r.Client.List(ctx, &revisionsList, client.InNamespace(appDeploy.GetNamespace()), client.MatchingFields{
				".metadata.ownerReferences": string(appDeploy.GetUID()),
			}); err != nil {
				fmt.Println(fmt.Errorf("failed to list revisions: %w", err))
			}

			for _, rev := range revisionsList.Items {
				if rev.Spec.Id == revision.Spec.Id {
					continue
				}
				rev.Status.Phase = "Non-active"

				if err := r.Client.Status().Update(ctx, &rev); err != nil {
					fmt.Println(fmt.Errorf("failed to update status for revision %s: %w", revision.Name, err))
				}

				rev.Spec.Status = "Non-active"
				if err := r.Client.Update(ctx, &rev); err != nil {
					fmt.Println(fmt.Errorf("failed to update spec status for revision %s: %w", revision.Name, err))
				}
			}

			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				// Attempt to update
				revision.Status.Phase = "Active"
				return r.Status().Update(ctx, &revision)
			})

			if err != nil {
				fmt.Println("Failed to update Revision instance after retries:", err)
				return false, false, err
			}

			// Update the existing Deployment if necessary
			found.Spec.Replicas = &revision.Spec.Replicas
			found.Spec.Template.Spec.Containers[0].Image = revision.Spec.Image
			found.Spec.Template.Spec.Containers[0].Resources = revision.Spec.Resources
			// TODO: also do changes for another fields

			err = r.Update(ctx, found)
			if err != nil {
				return createNewApp, createNewRevision, err
			}

			// TODO: set another revision to be inactive and sync to mongodb
			lyraClient := lyra.NewLyraClient(r.Client, revision.Namespace)
			accountSecret := &corev1.Secret{}
			if err := r.Client.Get(context.Background(), types.NamespacedName{Name: "lyrid.secretkey", Namespace: revision.Namespace}, accountSecret); err != nil {
				if errors.IsNotFound(err) {
					fmt.Printf("lyrid.secretkey secret is not found in namespace %s\n", revision.Namespace)
					return createNewApp, createNewRevision, err
				}
				return createNewApp, createNewRevision, err
			}
			_, err = lyraClient.SyncRevision(revision, string(accountSecret.Data["key"]), string(accountSecret.Data["secret"]))
			if err != nil {
				return createNewApp, createNewRevision, err
			}

			createNewRevision = true
		}

	} else {
		// TODO: remove
	}

	// TODO: clean
	return createNewApp, createNewRevision, nil
}
