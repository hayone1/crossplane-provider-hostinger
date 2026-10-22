package vps

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hostinger/terraform-provider-hostinger/hostinger"
)

// deleteVPS replaces the VPS Delete. Upstream cancels the subscription with
// DELETE /api/billing/v1/subscriptions/{id}, which the API no longer serves
// (HTTP 404), so a VPS could never be deleted. The API can only turn off
// auto-renewal now: the VPS keeps running until the end of the paid period
// and is then removed. A subscription whose auto-renewal is off already
// counts as deleted, so a repeated delete succeeds.
func deleteVPS(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	c, ok := meta.(*hostinger.HostingerClient)
	if !ok {
		return diag.Errorf("unexpected provider meta %T", meta)
	}
	id, err := strconv.Atoi(d.Id())
	if err != nil {
		return diag.Errorf("invalid VPS ID %q", d.Id())
	}
	vm, err := c.GetVirtualMachine(id)
	if errors.Is(err, hostinger.ErrNotFound) {
		d.SetId("")
		return nil
	}
	if err != nil {
		return diag.FromErr(fmt.Errorf("failed to get VPS %d: %w", id, err))
	}
	if vm.SubscriptionID == "" {
		return diag.Errorf("VPS %d has no subscription", id)
	}
	path := fmt.Sprintf("/api/billing/v1/subscriptions/%s/auto-renewal/disable", url.PathEscape(vm.SubscriptionID))
	if err := callJSON(ctx, c, http.MethodDelete, path, nil, nil); err != nil {
		if renews, lookupErr := autoRenewed(ctx, c, vm.SubscriptionID); lookupErr != nil || renews {
			return diag.FromErr(fmt.Errorf("failed to turn off auto-renewal of subscription %s: %w", vm.SubscriptionID, err))
		}
	}
	d.SetId("")
	return nil
}

func autoRenewed(ctx context.Context, c *hostinger.HostingerClient, subscription string) (bool, error) {
	var subs []struct {
		ID            string `json:"id"`
		IsAutoRenewed bool   `json:"is_auto_renewed"`
	}
	if err := callJSON(ctx, c, http.MethodGet, "/api/billing/v1/subscriptions", nil, &subs); err != nil {
		return false, fmt.Errorf("failed to list subscriptions: %w", err)
	}
	for _, s := range subs {
		if s.ID == subscription {
			return s.IsAutoRenewed, nil
		}
	}
	return false, fmt.Errorf("subscription %s not found", subscription)
}
