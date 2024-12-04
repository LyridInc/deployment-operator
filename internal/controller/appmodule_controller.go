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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	lyrmodel "github.com/LyridInc/go-sdk/model"
	appsv1alpha1 "github.com/LyridInc/lyrid-operator/api/v1alpha1"
	"github.com/LyridInc/lyrid-operator/pkg/lyra"
)

var operatorStartTime = time.Now()

// AppModuleReconciler reconciles a AppModule object
type AppModuleReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	K8sClient *kubernetes.Clientset
}

//+kubebuilder:rbac:groups=apps.lyrid.io,resources=appmodules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps.lyrid.io,resources=appmodules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps.lyrid.io,resources=appmodules/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AppModule object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *AppModuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	accountSecret := &corev1.Secret{}
	if err := r.Client.Get(context.Background(), types.NamespacedName{Name: "lyrid.secretkey", Namespace: req.Namespace}, accountSecret); err != nil {
		if errors.IsNotFound(err) {
			fmt.Printf("lyrid.secretkey secret is not found in namespace %s\n", req.Namespace)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	appModule := &appsv1alpha1.AppModule{}
	if err := r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, appModule); err != nil {
		if errors.IsNotFound(err) {
			log.Info("AppModule is deleted ->")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if appModule.Generation != appModule.Status.ObservedGeneration {
		defer func() {
			appModule.Status.ObservedGeneration = appModule.Generation
			if err := r.Client.Status().Update(ctx, appModule); err != nil {
				fmt.Println("Update generation error:", err)
			}
		}()
	} else {
		return ctrl.Result{}, nil
	}

	if origin, ok := appModule.Annotations["origin"]; ok && origin == "app-deployment" {
		appModule.Annotations["origin"] = ""
		if err := r.Client.Update(ctx, appModule); err != nil {
			if !errors.IsAlreadyExists(err) {
				log.Error(err, "Failed to update Module instance")
				return ctrl.Result{}, err
			}
			log.Error(err, "Failed to update Module instance")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	appDeploy := &appsv1alpha1.AppDeployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: appModule.Spec.Ref.AppDeployment["name"], Namespace: req.Namespace}, appDeploy); err != nil {
		if errors.IsNotFound(err) {
			log.Info("AppDeployment is deleted ->")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	lyraClient := lyra.NewLyraClient(r.Client, req.Namespace)
	syncModuleResponse, err := lyraClient.SyncModule(*appDeploy, *appModule, string(accountSecret.Data["key"]), string(accountSecret.Data["secret"]))
	if err != nil {
		return ctrl.Result{}, err
	}

	if syncModuleResponse.ModuleRevision.ID != "" {
		fmt.Println("---- Sync Module-Revision ----")
		moduleHandleRevisionChanges(ctx, r, *appDeploy, *syncModuleResponse)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AppModuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1alpha1.AppModule{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

func moduleHandleRevisionChanges(ctx context.Context, r *AppModuleReconciler, appDeploy appsv1alpha1.AppDeployment, syncModule lyrmodel.SyncModuleResponse) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if appDeploy.Spec.CurrentRevisionId != syncModule.ModuleRevision.ID {
		appDeploy.Spec.CurrentRevisionId = syncModule.ModuleRevision.ID
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

		for _, revision := range revisionsList.Items {
			revision.Status.Phase = "Non-active"

			if err := r.Client.Status().Update(ctx, &revision); err != nil {
				fmt.Println(fmt.Errorf("failed to update status for revision %s: %w", revision.Name, err))
			}
		}

		var spec = appDeploy.Spec
		newRevision := &appsv1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"source": "lyrid-operator",
				},
			},
			Spec: appsv1alpha1.RevisionSpec{
				Id:           syncModule.ModuleRevision.ID,
				Image:        spec.Image,
				ModuleId:     syncModule.Module.ID,
				Ports:        spec.Ports,
				Replicas:     spec.Replicas,
				Resources:    spec.Resources,
				VolumeMounts: spec.VolumeMounts,
				Ref: appsv1alpha1.AppDeploymentRef{
					AppDeployment: map[string]string{
						"name": appDeploy.Name,
					},
				},
			},
		}
		newRevision.SetName(syncModule.ModuleRevision.Title)
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

		revisionsCount := len(revisionsList.Items)
		if revisionsCount >= 3 {
			sort.Slice(revisionsList.Items, func(i, j int) bool {
				return revisionsList.Items[i].CreationTimestamp.After(revisionsList.Items[j].CreationTimestamp.Time)
			})

			deletedRevision := revisionsList.Items[revisionsCount-1]

			if err := r.Client.Delete(ctx, &deletedRevision); err != nil {
				fmt.Println(fmt.Errorf("failed to delete revision %s: %w", deletedRevision.Name, err))
			}

			// delete function, function code, and create new

			if _, err := moduleCreateFunction(ctx, r, appDeploy, syncModule); err != nil {
				log.Error(err, "Failed to create function")
				return ctrl.Result{}, err
			}
		}
	} else if appDeploy.Spec.CurrentRevisionId == "" {
		if _, err := moduleCreateFunction(ctx, r, appDeploy, syncModule); err != nil {
			log.Error(err, "Failed to create function")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func moduleCreateFunction(ctx context.Context, r *AppModuleReconciler, appDeploy appsv1alpha1.AppDeployment, syncModule lyrmodel.SyncModuleResponse) (ctrl.Result, error) {

	log := log.FromContext(ctx)
	newFunction := &appsv1alpha1.Function{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"source": "lyrid-operator",
			},
		},
		Spec: appsv1alpha1.FunctionSpec{
			Id:          syncModule.Function.ID,
			ModuleId:    syncModule.Function.ModuleID,
			RevisionId:  syncModule.Function.RevisionID,
			Name:        syncModule.Function.Name,
			Description: syncModule.Function.Description,
			Ref: appsv1alpha1.AppDeploymentRef{
				AppDeployment: map[string]string{
					"name": appDeploy.Name,
				},
			},
		},
	}
	newFunction.SetName(syncModule.Function.Name)
	newFunction.SetNamespace(appDeploy.Namespace)

	if err := controllerutil.SetControllerReference(
		&appDeploy,
		newFunction,
		r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	// Create the instance if it does not exist
	if err := r.Client.Create(ctx, newFunction); err != nil {
		if !errors.IsAlreadyExists(err) {
			log.Error(err, "Failed to create new Function instance: Already exist")
			return ctrl.Result{}, err
		}
		log.Error(err, "Failed to create new Function instance")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
