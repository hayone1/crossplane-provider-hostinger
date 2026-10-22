package vps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hostinger/terraform-provider-hostinger/hostinger"
)

// pollInterval is how often createVPS checks on a pending purchase or setup.
var pollInterval = 15 * time.Second

// stateInitial is the state of a VPS that is paid for but not set up yet.
const stateInitial = "initial"

// createVPS replaces the VPS Create. Upstream treats the purchase API's
// HTTP 202 (payment still being processed) as a failure, so Crossplane
// retried the create and every retry bought another VPS; the setup sent with
// the purchase is also lost then, leaving the VPS in the "initial" state.
//
// Here a VPS in the "initial" state is adopted instead of buying a new one, so
// a retried create never buys twice. Otherwise the VPS is purchased, and on a
// 202 createVPS waits for it to appear. A VPS that is not set up yet is then
// set up, and createVPS waits until it is running.
func createVPS(read schema.ReadContextFunc) schema.CreateContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		c, ok := meta.(*hostinger.HostingerClient)
		if !ok {
			return diag.Errorf("unexpected provider meta %T", meta)
		}
		vms, err := c.GetVirtualMachines()
		if err != nil {
			return diag.FromErr(fmt.Errorf("failed to list VPSs: %w", err))
		}
		vm := firstInitial(vms)
		if vm == nil {
			if vm, err = purchase(ctx, c, d, vms); err != nil {
				return diag.FromErr(err)
			}
		}
		// Until setup is accepted the VPS stays "initial", so a failed create
		// adopts it again on retry. From then on the ID is recorded: Upjet keeps
		// it even if the create fails later, so a retry never buys again.
		if vm.State == stateInitial {
			if err := setup(ctx, c, vm.ID, d); err != nil {
				return diag.FromErr(err)
			}
		}
		d.SetId(strconv.Itoa(vm.ID))
		if err := d.Set("vps_id", vm.ID); err != nil {
			return diag.FromErr(err)
		}
		if err := waitRunning(ctx, c, vm.ID); err != nil {
			return diag.FromErr(err)
		}

		if err := attachKeys(ctx, c, vm.ID, sshKeyIDs(d)); err != nil {
			return diag.FromErr(err)
		}
		return read(ctx, d, meta)
	}
}

func firstInitial(vms []hostinger.VirtualMachine) *hostinger.VirtualMachine {
	for i := range vms {
		if vms[i].State == stateInitial {
			return &vms[i]
		}
	}
	return nil
}

// purchase buys a VPS and returns it. On HTTP 202 it waits until a VPS that
// is not in before shows up.
func purchase(ctx context.Context, c *hostinger.HostingerClient, d *schema.ResourceData, before []hostinger.VirtualMachine) (*hostinger.VirtualMachine, error) {
	req := hostinger.PurchaseVPSRequest{
		ItemID: d.Get("plan").(string),
		Setup:  setupFields(d),
	}
	if v, ok := d.GetOk("payment_method_id"); ok {
		id := v.(int)
		req.PaymentMethodID = &id
	}
	status, body, err := call(ctx, c, http.MethodPost, "/api/vps/v1/virtual-machines", req)
	if err != nil {
		return nil, fmt.Errorf("failed to purchase VPS: %w", err)
	}
	switch status {
	case http.StatusOK, http.StatusCreated:
		var res hostinger.PurchaseVPSResponse
		if err := json.Unmarshal(body, &res); err != nil {
			return nil, fmt.Errorf("invalid purchase response: %w", err)
		}
		return &res.VirtualMachine, nil
	case http.StatusAccepted:
		return waitNew(ctx, c, before)
	default:
		return nil, fmt.Errorf("failed to purchase VPS (HTTP %d): %s", status, body)
	}
}

func setupFields(d *schema.ResourceData) hostinger.PurchaseVPSSetup {
	s := hostinger.PurchaseVPSSetup{
		DataCenterID: d.Get("data_center_id").(int),
		TemplateID:   d.Get("template_id").(int),
	}
	if v, ok := d.GetOk("password"); ok {
		pw := v.(string)
		s.Password = &pw
	}
	if v, ok := d.GetOk("hostname"); ok {
		h := v.(string)
		s.Hostname = &h
	}
	if v, ok := d.GetOk("post_install_script_id"); ok {
		id := v.(int)
		s.PostInstallScriptID = &id
	}
	return s
}

func waitNew(ctx context.Context, c *hostinger.HostingerClient, before []hostinger.VirtualMachine) (*hostinger.VirtualMachine, error) {
	known := map[int]bool{}
	for _, vm := range before {
		known[vm.ID] = true
	}
	for {
		vms, err := c.GetVirtualMachines()
		if err != nil {
			return nil, fmt.Errorf("failed to list VPSs: %w", err)
		}
		for i := range vms {
			if !known[vms[i].ID] {
				return &vms[i], nil
			}
		}
		if err := sleep(ctx); err != nil {
			return nil, fmt.Errorf("purchase is still pending, the VPS will be adopted once it appears: %w", err)
		}
	}
}

func setup(ctx context.Context, c *hostinger.HostingerClient, id int, d *schema.ResourceData) error {
	path := fmt.Sprintf("/api/vps/v1/virtual-machines/%d/setup", id)
	if err := callJSON(ctx, c, http.MethodPost, path, setupFields(d), nil); err != nil {
		return fmt.Errorf("failed to set up VPS %d: %w", id, err)
	}
	return nil
}

func waitRunning(ctx context.Context, c *hostinger.HostingerClient, id int) error {
	// Errors are retried: giving up here would fail a create whose VPS is
	// being installed fine.
	state := "unknown"
	for {
		vm, err := c.GetVirtualMachine(id)
		if err == nil {
			if vm.State == "running" {
				return nil
			}
			state = vm.State
		}
		if err := sleep(ctx); err != nil {
			return fmt.Errorf("VPS %d is %q, not running: %w", id, state, err)
		}
	}
}

func sleep(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(pollInterval):
		return nil
	}
}

// callJSON sends a request to the Hostinger API, fails unless the status is
// 2xx, and decodes the response into out unless it is nil.
func callJSON(ctx context.Context, c *hostinger.HostingerClient, method, path string, in, out any) error {
	status, body, err := call(ctx, c, method, path, in)
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return fmt.Errorf("%s %s: HTTP %d: %s", method, path, status, body)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s %s: invalid response: %w", method, path, err)
	}
	return nil
}

// call sends a request to the Hostinger API and returns the status and body.
func call(ctx context.Context, c *hostinger.HostingerClient, method, path string, in any) (int, []byte, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	return resp.StatusCode, out, err
}
