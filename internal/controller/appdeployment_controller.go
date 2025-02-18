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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	lyrmodel "github.com/LyridInc/go-sdk/model"
	appsv1alpha1 "github.com/LyridInc/lyrid-operator/api/v1alpha1"
	"github.com/LyridInc/lyrid-operator/pkg/lyra"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AppDeploymentReconciler reconciles a AppDeployment object
type AppDeploymentReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	K8sClient          *kubernetes.Clientset
	RevisionReconciler *RevisionReconciler
}

//+kubebuilder:rbac:groups=apps.lyrid.io,resources=appdeployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps.lyrid.io,resources=appdeployments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps.lyrid.io,resources=appdeployments/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AppDeployment object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *AppDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	accountSecret := &corev1.Secret{}
	if err := r.Client.Get(context.Background(), types.NamespacedName{Name: "lyrid.secretkey", Namespace: req.Namespace}, accountSecret); err != nil {
		if errors.IsNotFound(err) {
			fmt.Printf("lyrid.secretkey secret is not found in namespace %s\n", req.Namespace)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	lyraClient := lyra.NewLyraClient(r.Client, req.Namespace)

	appDeploy := &appsv1alpha1.AppDeployment{}
	if err := r.Get(ctx, req.NamespacedName, appDeploy); err != nil {
		if errors.IsNotFound(err) {
			log.Info("AppDeployment is deleted ->")
			appDeploy.Name = req.Name
			appDeploy.Namespace = req.Namespace
			deleteAppResponse, err := lyraClient.DeleteApp(*appDeploy, string(accountSecret.Data["key"]), string(accountSecret.Data["secret"]))
			if err != nil {
				fmt.Println("Response:", deleteAppResponse)
				fmt.Println("Error:", err)
				return ctrl.Result{}, err
			}

			// delete service
			if err := r.K8sClient.CoreV1().Services(appDeploy.Namespace).Delete(ctx, appDeploy.Name, metav1.DeleteOptions{}); err != nil {
				fmt.Println("Delete service error:", err)
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	fmt.Printf("------------------------------------- RECONCILE APPDEPLOYMENT: %s -------------------------------------\n", appDeploy.Name)
	defer func() {
		fmt.Printf("------------------------------------- RECONCILE APPDEPLOYMENT: %s FINISHED ------------------------------------\n", appDeploy.Name)
	}()

	fmt.Println("Current AppDeploy revision id:", appDeploy.Spec.CurrentRevisionId)
	isCreateNewAppModule, isCreateNewRevision, _ := handleAppDeploymentChanges(ctx, r, *appDeploy)
	if isCreateNewRevision {
		appDeploy.Spec.CurrentRevisionId = "" // empty the revision id so that it will trigger new revision creation
	}

	syncAppResponse, err := lyraClient.SyncApp(*appDeploy, string(accountSecret.Data["key"]), string(accountSecret.Data["secret"]))
	if err != nil {
		return ctrl.Result{}, err
	}

	if isCreateNewAppModule {
		// new module
		newAppModule := &appsv1alpha1.AppModule{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"source": "lyrid-operator",
					"origin": "app-deployment",
				},
			},
			Spec: appsv1alpha1.AppModuleSpec{
				Id:          syncAppResponse.Module.ID,
				Name:        appDeploy.Name + "-module",
				AppId:       syncAppResponse.App.ID,
				Language:    "Docker",
				Web:         "-",
				Description: "App module from operator",
				Ref: appsv1alpha1.AppDeploymentRef{
					AppDeployment: map[string]string{
						"name": appDeploy.Name,
					},
				},
			},
		}
		newAppModule.SetName(appDeploy.Name + "-module")
		newAppModule.SetNamespace(appDeploy.Namespace)

		if err := controllerutil.SetControllerReference(
			appDeploy,
			newAppModule,
			r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		// Create the instance if it does not exist
		if err := r.Client.Create(ctx, newAppModule); err != nil {
			if !errors.IsAlreadyExists(err) {
				log.Error(err, "Failed to create new Module instance")
				return ctrl.Result{}, err
			}
			log.Error(err, "Failed to create new Module instance")
			return ctrl.Result{}, err
		}

		// new subdomain
		newSubdomain := &appsv1alpha1.Subdomain{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"source": "lyrid-operator",
				},
			},
			Spec: appsv1alpha1.SubdomainSpec{
				Name:         syncAppResponse.Subdomain.Name,
				AppId:        syncAppResponse.Subdomain.AppId,
				AccountId:    syncAppResponse.App.AccountId,
				ModuleId:     syncAppResponse.Subdomain.ModuleId,
				FunctionName: syncAppResponse.Subdomain.FunctionName,
				Tag:          syncAppResponse.Subdomain.Tag,
				// Public:       *syncAppResponse.Subdomain.Public,
				Ref: appsv1alpha1.AppDeploymentRef{
					AppDeployment: map[string]string{
						"name": appDeploy.Name,
					},
				},
			},
		}
		newSubdomain.SetName(syncAppResponse.Subdomain.Name)
		newSubdomain.SetNamespace(appDeploy.Namespace)

		if err := controllerutil.SetControllerReference(
			appDeploy,
			newSubdomain,
			r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		// Create the instance if it does not exist
		if err := r.Client.Create(ctx, newSubdomain); err != nil {
			if !errors.IsAlreadyExists(err) {
				log.Error(err, "Failed to create new Subdomain instance")
				return ctrl.Result{}, err
			}
			log.Error(err, "Failed to create new Subdomain instance")
			return ctrl.Result{}, err
		}
	}

	if syncAppResponse.ModuleRevision.ID != "" {
		defer func() {
			handleRevisionChanges(ctx, r, *appDeploy, *syncAppResponse)
		}()
	}

	if isCreateNewRevision {
		handleServiceChanges(ctx, r, *appDeploy)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AppDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.RevisionReconciler = &RevisionReconciler{}
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1alpha1.AppDeployment{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				// Custom logic before creating an AppDeployment
				fmt.Println("create event")
				return true
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				// Custom logic before updating an AppDeployment
				fmt.Println("update event")
				return true
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				// Custom logic before deleting an AppDeployment
				fmt.Println("delete event")
				return true
			},
		}).
		Complete(r)
}

func handleAppDeploymentChanges(ctx context.Context, r *AppDeploymentReconciler, appDeploy appsv1alpha1.AppDeployment) (bool, bool, error) {
	container := corev1.Container{
		Name:      appDeploy.Name,
		Image:     appDeploy.Spec.Image,
		Resources: appDeploy.Spec.Resources,
		Ports:     appDeploy.Spec.Ports,
	}

	if len(appDeploy.Spec.VolumeMounts) > 0 {
		container.VolumeMounts = appDeploy.Spec.VolumeMounts
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appDeploy.Name,
			Namespace: appDeploy.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &appDeploy.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": appDeploy.Name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": appDeploy.Name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						container,
					},
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(&appDeploy, dep, r.Scheme); err != nil {
		return false, false, err
	}

	var createNewApp bool
	var createNewRevision bool
	found := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}, found); err != nil {
		// If the Deployment doesn't exist, create it
		err = r.Create(ctx, dep)
		if err != nil {
			return createNewApp, createNewRevision, err
		}
		createNewRevision = true
		createNewApp = true
	} else {
		// Update the existing Deployment if necessary
		if *found.Spec.Replicas != appDeploy.Spec.Replicas ||
			found.Spec.Template.Spec.Containers[0].Image != appDeploy.Spec.Image ||
			!compareResources(found.Spec.Template.Spec.Containers[0].Resources, appDeploy.Spec.Resources) {
			found.Spec.Replicas = &appDeploy.Spec.Replicas
			found.Spec.Template.Spec.Containers[0].Image = appDeploy.Spec.Image
			found.Spec.Template.Spec.Containers[0].Resources = appDeploy.Spec.Resources
			// TODO: also do changes for another fields

			err = r.Update(ctx, found)
			if err != nil {
				return createNewApp, createNewRevision, err
			}

			createNewRevision = true
		}

	}
	return createNewApp, createNewRevision, nil
}

func handleServiceChanges(ctx context.Context, r *AppDeploymentReconciler, appDeploy appsv1alpha1.AppDeployment) (ctrl.Result, error) {
	ports := []corev1.ServicePort{}
	for _, p := range appDeploy.Spec.Ports {
		ports = append(ports, corev1.ServicePort{
			Name:       p.Name,
			Protocol:   p.Protocol,
			Port:       80,
			TargetPort: intstr.FromInt(int(p.ContainerPort)),
		})
	}

	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      appDeploy.Name,
			Namespace: appDeploy.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: ports,
			Selector: map[string]string{
				"app": appDeploy.Name,
			},
			Type: "ClusterIP",
		},
	}

	existingService, err := r.K8sClient.CoreV1().Services(appDeploy.Namespace).Get(ctx, service.Name, metav1.GetOptions{})
	if err == nil && existingService != nil {
		if _, err := r.K8sClient.CoreV1().Services(appDeploy.Namespace).Update(ctx, service, metav1.UpdateOptions{}); err != nil {
			fmt.Println(err)
			return ctrl.Result{}, err
		}
	} else {
		if _, err := r.K8sClient.CoreV1().Services(appDeploy.Namespace).Create(ctx, service, metav1.CreateOptions{}); err != nil {
			fmt.Println(err)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, err
}

func handleRevisionChanges(ctx context.Context, r *AppDeploymentReconciler, appDeploy appsv1alpha1.AppDeployment, syncApp lyrmodel.SyncAppResponse) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	fmt.Println("App Deploy:", appDeploy.Name)
	fmt.Println("Current:", appDeploy.Spec.CurrentRevisionId)
	fmt.Println("New:", syncApp.ModuleRevision.ID)

	changes, ok := appDeploy.Annotations["changes"]
	if ok {
		fmt.Println("Changes:", changes)
	}

	if appDeploy.Spec.CurrentRevisionId != syncApp.ModuleRevision.ID || changes == "revision-reconciler" {
		appDeploy.Spec.CurrentRevisionId = syncApp.ModuleRevision.ID
		if changes != "" {
			appDeploy.Annotations["changes"] = ""
			fmt.Printf("AppDeployment -> Emptying app deployment %s annotation changes\n", appDeploy.Name)
			if err := r.Update(ctx, &appDeploy); err != nil {
				log.Error(err, "Failed to update current revision id on app deployment")
			}
		}

		if !ok || changes != "revision-reconciler" {
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

				revision.Spec.Status = "Non-active"
				if err := r.Client.Update(ctx, &revision); err != nil {
					fmt.Println(fmt.Errorf("failed to update spec status for revision %s: %w", revision.Name, err))
				}
			}

			var spec = appDeploy.Spec
			newRevision := &appsv1alpha1.Revision{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"source": "lyrid-operator",
						"origin": "app-deployment",
					},
				},
				Spec: appsv1alpha1.RevisionSpec{
					Id:           syncApp.ModuleRevision.ID,
					Image:        spec.Image,
					ModuleId:     syncApp.Module.ID,
					Ports:        spec.Ports,
					Replicas:     spec.Replicas,
					Resources:    spec.Resources,
					VolumeMounts: spec.VolumeMounts,
					Ref: appsv1alpha1.AppDeploymentRef{
						AppDeployment: map[string]string{
							"name": appDeploy.Name,
						},
					},
					Status: "Active",
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
			fmt.Println("Create new revision with id:", newRevision.Spec.Id)
			if err := r.Client.Create(ctx, newRevision); err != nil {
				if !errors.IsAlreadyExists(err) {
					log.Error(err, "Failed to create new Revision instance")
					return ctrl.Result{}, err
				}
				log.Error(err, "Failed to create new Revision instance")
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

			}
		}

	} else if appDeploy.Spec.CurrentRevisionId == "" {
		if _, err := createFunction(ctx, r, appDeploy, syncApp); err != nil {
			log.Error(err, "Failed to create function")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func createFunction(ctx context.Context, r *AppDeploymentReconciler, appDeploy appsv1alpha1.AppDeployment, syncApp lyrmodel.SyncAppResponse) (ctrl.Result, error) {

	log := log.FromContext(ctx)
	newFunction := &appsv1alpha1.Function{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"source": "lyrid-operator",
			},
		},
		Spec: appsv1alpha1.FunctionSpec{
			Id:          syncApp.Function.ID,
			ModuleId:    syncApp.Function.ModuleID,
			RevisionId:  syncApp.Function.RevisionID,
			Name:        syncApp.Function.Name,
			Description: syncApp.Function.Description,
			Ref: appsv1alpha1.AppDeploymentRef{
				AppDeployment: map[string]string{
					"name": appDeploy.Name,
				},
			},
		},
	}
	newFunction.SetName(syncApp.Function.Name)
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

func compareResources(current, desired corev1.ResourceRequirements) bool {
	// Compare CPU requests
	if current.Requests[corev1.ResourceCPU] != desired.Requests[corev1.ResourceCPU] {
		return false
	}

	// Compare memory requests
	if current.Requests[corev1.ResourceMemory] != desired.Requests[corev1.ResourceMemory] {
		return false
	}

	// Compare CPU limits
	if current.Limits[corev1.ResourceCPU] != desired.Limits[corev1.ResourceCPU] {
		return false
	}

	// Compare memory limits
	if current.Limits[corev1.ResourceMemory] != desired.Limits[corev1.ResourceMemory] {
		return false
	}

	return true
}
