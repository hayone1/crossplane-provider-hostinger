// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/crossplane/upjet/v2/pkg/controller"

	record "github.com/hayone1/crossplane-provider-hostinger/internal/controller/cluster/dns/record"
	providerconfig "github.com/hayone1/crossplane-provider-hostinger/internal/controller/cluster/providerconfig"
	postinstallscript "github.com/hayone1/crossplane-provider-hostinger/internal/controller/cluster/vps/postinstallscript"
	sshkey "github.com/hayone1/crossplane-provider-hostinger/internal/controller/cluster/vps/sshkey"
	vps "github.com/hayone1/crossplane-provider-hostinger/internal/controller/cluster/vps/vps"
)

// Setup creates all controllers with the supplied logger and adds them to
// the supplied manager.
func Setup(mgr ctrl.Manager, o controller.Options) error {
	for _, setup := range []func(ctrl.Manager, controller.Options) error{
		record.Setup,
		providerconfig.Setup,
		postinstallscript.Setup,
		sshkey.Setup,
		vps.Setup,
	} {
		if err := setup(mgr, o); err != nil {
			return err
		}
	}
	return nil
}

// SetupGated creates all controllers with the supplied logger and adds them to
// the supplied manager gated.
func SetupGated(mgr ctrl.Manager, o controller.Options) error {
	for _, setup := range []func(ctrl.Manager, controller.Options) error{
		record.SetupGated,
		providerconfig.SetupGated,
		postinstallscript.SetupGated,
		sshkey.SetupGated,
		vps.SetupGated,
	} {
		if err := setup(mgr, o); err != nil {
			return err
		}
	}
	return nil
}

// SetupWebhookWithManager registers conversion webhooks for all resource kinds in the group.
func SetupWebhookWithManager(mgr ctrl.Manager) error {
	for _, setup := range []func(ctrl.Manager) error{
		record.SetupWebhookWithManager,
		providerconfig.SetupWebhookWithManager,
		postinstallscript.SetupWebhookWithManager,
		sshkey.SetupWebhookWithManager,
		vps.SetupWebhookWithManager,
	} {
		if err := setup(mgr); err != nil {
			return err
		}
	}
	return nil
}
