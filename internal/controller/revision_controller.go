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
	"sort"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	lyrmodel "github.com/LyridInc/go-sdk/model"
	appsv1alpha1 "github.com/LyridInc/lyrid-operator/api/v1alpha1"
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
	_ = log.FromContext(ctx)

	fmt.Println("------------------------------------- RECONCILE REVISION HERE -------------------------------------")
	revision := appsv1alpha1.Revision{}
	err := r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, &revision)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
	} else if revision.Status.Phase == "Active" {
		fmt.Println(req.Name, revision.Name)
		appDeploy := appsv1alpha1.AppDeployment{}
		if err := r.Get(ctx, types.NamespacedName{Name: revision.Spec.Ref.AppDeployment["name"], Namespace: req.Namespace}, &appDeploy); err == nil {
			if appDeploy.Spec.CurrentRevisionId != revision.Spec.Id {
				fmt.Println("Changes in revision")
			}
		}
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

func revisionHandleRevisionChanges(ctx context.Context, r *RevisionReconciler, appDeploy appsv1alpha1.AppDeployment, syncApp lyrmodel.SyncAppResponse) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	fmt.Println("Current:", appDeploy.Spec.CurrentRevisionId)
	fmt.Println("New:", syncApp.ModuleRevision.ID)

	if appDeploy.Spec.CurrentRevisionId != syncApp.ModuleRevision.ID {
		appDeploy.Spec.CurrentRevisionId = syncApp.ModuleRevision.ID
		if err := r.Update(ctx, &appDeploy); err != nil {
			log.Error(err, "Failed to update current revision id on app deployment")
			return ctrl.Result{}, err
		}

		revisionsList := appsv1alpha1.RevisionList{}

		if err := r.Client.List(ctx, &revisionsList, client.InNamespace(appDeploy.GetNamespace()), client.MatchingFields{
			".metadata.ownerReferences": string(appDeploy.GetUID()),
		}); err != nil {
			fmt.Println(fmt.Errorf("failed to list revisions: %w", err))
		}

		revisionsCount := len(revisionsList.Items)
		if revisionsCount >= 3 {
			sort.Slice(revisionsList.Items, func(i, j int) bool {
				return revisionsList.Items[i].CreationTimestamp.After(revisionsList.Items[j].CreationTimestamp.Time)
			})

			deletedRevision := revisionsList.Items[revisionsCount-1]
			if err := r.Client.Delete(ctx, &deletedRevision); err != nil {
				fmt.Println(fmt.Errorf("failed to delete revision %s: %w", deletedRevision.Name, err))
			}
		}

		for _, revision := range revisionsList.Items {
			revision.Status.Phase = "Non-active"

			if err := r.Client.Status().Update(ctx, &revision); err != nil {
				fmt.Println(fmt.Errorf("failed to update status for revision %s: %w", revision.Name, err))
			}
		}

		var spec = appDeploy.Spec
		newRevision := &appsv1alpha1.Revision{
			Spec: appsv1alpha1.RevisionSpec{
				Image:        spec.Image,
				ModuleId:     syncApp.Module.ID,
				Ports:        spec.Ports,
				Replicas:     spec.Replicas,
				Resources:    spec.Resources,
				VolumeMounts: spec.VolumeMounts,
			},
		}
		newRevision.SetName(syncApp.ModuleRevision.Title)
		newRevision.SetNamespace(appDeploy.Namespace)

		if err := controllerutil.SetControllerReference(
			&appDeploy,
			newRevision,
			r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		// Create the instance if it does not exist
		if err := r.Client.Create(ctx, newRevision); err != nil {
			if !errors.IsAlreadyExists(err) {
				log.Error(err, "Failed to create new Revision instance")
				return ctrl.Result{}, err
			}
			log.Error(err, "Failed to create new Revision instance")
			return ctrl.Result{}, err
		}

		newRevision.Status.Phase = "Active"
		newRevision.Status.Message = "Revision is up and running"
		newRevision.Status.Conditions = []metav1.Condition{
			{
				LastTransitionTime: metav1.Now(),
				Message:            newRevision.Status.Message,
				Type:               newRevision.Status.Phase,
				Status:             "true",
			},
		}
		if err := r.Status().Update(ctx, newRevision); err != nil {
			log.Error(err, "Failed to update status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}
