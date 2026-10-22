package vps

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hostinger/terraform-provider-hostinger/hostinger"
)

// updateVPS replaces the VPS Update. Upstream lists and attaches SSH keys
// through deprecated endpoints: the list one is gone (HTTP 404), so every
// update of ssh_key_ids failed, and the attach one answers 200 without
// installing the key. Hostname and template changes are as upstream.
func updateVPS(read schema.ReadContextFunc) schema.UpdateContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		c, ok := meta.(*hostinger.HostingerClient)
		if !ok {
			return diag.Errorf("unexpected provider meta %T", meta)
		}
		vmID, err := strconv.Atoi(d.Id())
		if err != nil {
			return diag.Errorf("invalid VPS ID %q", d.Id())
		}
		if d.HasChange("hostname") {
			if err := c.UpdateHostname(vmID, d.Get("hostname").(string)); err != nil {
				return diag.FromErr(fmt.Errorf("failed to update hostname: %w", err))
			}
		}
		if d.HasChange("template_id") {
			if err := recreate(c, vmID, d); err != nil {
				return diag.FromErr(err)
			}
		}
		if d.HasChange("ssh_key_ids") {
			if err := attachKeys(ctx, c, vmID, sshKeyIDs(d)); err != nil {
				return diag.FromErr(err)
			}
		}
		return read(ctx, d, meta)
	}
}

// recreate reinstalls the VPS with the template in d.
func recreate(c *hostinger.HostingerClient, vmID int, d *schema.ResourceData) error {
	s := setupFields(d)
	if err := c.RecreateVirtualMachine(vmID, s.TemplateID, s.Password, s.PostInstallScriptID); err != nil {
		return fmt.Errorf("failed to recreate VPS: %w", err)
	}
	return nil
}

func sshKeyIDs(d *schema.ResourceData) []int {
	raw, _ := d.Get("ssh_key_ids").([]any)
	ids := make([]int, len(raw))
	for i, id := range raw {
		ids[i] = id.(int)
	}
	return ids
}

// attachKeys installs the account's SSH keys with the given IDs for root on
// the VPS, skipping keys that are installed already. Keys are only ever
// added, as upstream.
func attachKeys(ctx context.Context, c *hostinger.HostingerClient, vmID int, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	keys, err := accountKeys(ctx, c, ids)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/api/vps/v1/virtual-machines/%d/ssh-keys", vmID)
	var installed []struct {
		Key string `json:"key"`
	}
	if err := callJSON(ctx, c, http.MethodGet, path, nil, &installed); err != nil {
		return fmt.Errorf("failed to list SSH keys of VPS %d: %w", vmID, err)
	}
	have := map[string]bool{}
	for _, k := range installed {
		have[keyData(k.Key)] = true
	}
	var add []string
	for _, k := range keys {
		if !have[keyData(k)] {
			add = append(add, k)
		}
	}
	if len(add) == 0 {
		return nil
	}
	if err := callJSON(ctx, c, http.MethodPost, path, map[string][]string{"keys": add}, nil); err != nil {
		return fmt.Errorf("failed to add SSH keys to VPS %d: %w", vmID, err)
	}
	return nil
}

// keyData returns the type and data of an OpenSSH public key, without the
// comment, which the VPS may not keep.
func keyData(key string) string {
	f := strings.Fields(key)
	if len(f) < 2 {
		return key
	}
	return f[0] + " " + f[1]
}

// accountKeys returns the public keys with the given IDs from the account.
func accountKeys(ctx context.Context, c *hostinger.HostingerClient, ids []int) ([]string, error) {
	want := map[int]bool{}
	for _, id := range ids {
		want[id] = true
	}
	found := map[int]string{}
	for page := 1; ; page++ {
		var res struct {
			Data []struct {
				ID  int    `json:"id"`
				Key string `json:"key"`
			} `json:"data"`
			Meta struct {
				CurrentPage int `json:"current_page"`
				PerPage     int `json:"per_page"`
				Total       int `json:"total"`
			} `json:"meta"`
		}
		if err := callJSON(ctx, c, http.MethodGet, fmt.Sprintf("/api/vps/v1/public-keys?page=%d", page), nil, &res); err != nil {
			return nil, fmt.Errorf("failed to list public keys: %w", err)
		}
		for _, k := range res.Data {
			if want[k.ID] {
				found[k.ID] = k.Key
			}
		}
		if len(found) == len(want) || len(res.Data) == 0 || page*res.Meta.PerPage >= res.Meta.Total {
			break
		}
	}
	return inOrder(ids, found)
}

// inOrder returns the keys with the given IDs, in that order.
func inOrder(ids []int, keys map[int]string) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		k, ok := keys[id]
		if !ok {
			return nil, fmt.Errorf("public key %d not found", id)
		}
		out = append(out, k)
	}
	return out, nil
}
