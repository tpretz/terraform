package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/go-cleanhttp"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/hashicorp/terraform/backend"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/state"
	"github.com/hashicorp/terraform/state/remote"
)

func New() backend.Backend {
	s := &schema.Backend{
		Schema: map[string]*schema.Schema{
			"address": &schema.Schema{
				Type:        schema.TypeString,
				Required:    true,
				Description: "The address of the REST endpoint",
			},
			"update_method": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "POST",
				Description: "HTTP method to use when updating state",
			},
			"lock_address": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The address of the lock REST endpoint",
			},
			"unlock_address": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The address of the unlock REST endpoint",
			},
			"lock_method": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "LOCK",
				Description: "The HTTP method to use when locking",
			},
			"unlock_method": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "UNLOCK",
				Description: "The HTTP method to use when unlocking",
			},
			"username": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The username for HTTP basic authentication",
			},
			"password": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The password for HTTP basic authentication",
			},
			"skip_cert_verification": &schema.Schema{
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Whether to skip TLS verification.",
			},
			"retry_max": &schema.Schema{
				Type:        schema.TypeInt,
				Optional:    true,
				Default:     2,
				Description: "The number of HTTP request retries.",
			},
			"retry_wait_min": &schema.Schema{
				Type:        schema.TypeInt,
				Optional:    true,
				Default:     1,
				Description: "The minimum time in seconds to wait between HTTP request attempts.",
			},
			"retry_wait_max": &schema.Schema{
				Type:        schema.TypeInt,
				Optional:    true,
				Default:     30,
				Description: "The maximum time in seconds to wait between HTTP request attempts.",
			},
			"workspace_path_element": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The URL path string to replace with the active workspace name.",
			},
			"workspace_list_address": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The address of the workspace list REST endpoint.",
			},
			"headers": &schema.Schema{
				Type:     schema.TypeMap,
				Optional: true,
				Elem: &schema.Schema{
					Type:        schema.TypeString,
					Description: "Header Value",
				},
			},
		},
	}

	b := &Backend{Backend: s}
	b.Backend.ConfigureFunc = b.configure
	return b
}

type Backend struct {
	*schema.Backend

	client               *httpClient
	workspacePathElement string
	ctx                  context.Context
}

func (b *Backend) parseUrl(ctx context.Context, key string, optional bool, replaceOld string, replaceNew string) (*url.URL, error) {
	data := schema.FromContextBackendConfig(ctx)
	// if required, try to use
	// if optional, and not present, skip
	if v, ok := data.GetOk(key); (ok && v.(string) != "") || !optional {
		if replaceOld != "" {
			v = strings.ReplaceAll(v.(string), replaceOld, replaceNew)
		}
		return b.parseUrlValue(key, v.(string))
	}
	return nil, nil
}

func (b *Backend) parseUrlValue(key string, address string) (*url.URL, error) {
	urlObj, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s URL: %s", key, err)
	}
	if urlObj.Scheme != "http" && urlObj.Scheme != "https" {
		return nil, fmt.Errorf("%s must be HTTP or HTTPS", key)
	}
	return urlObj, nil
}

func (b *Backend) configure(ctx context.Context) error {
	data := schema.FromContextBackendConfig(ctx)
	b.ctx = ctx

	updateURL, err := b.parseUrl(ctx, "address", false, "", "")
	if err != nil {
		return err
	}
	updateMethod := data.Get("update_method").(string)

	lockURL, err := b.parseUrl(ctx, "lock_address", true, "", "")
	if err != nil {
		return err
	}
	lockMethod := data.Get("lock_method").(string)

	unlockURL, err := b.parseUrl(ctx, "unlock_address", true, "", "")
	if err != nil {
		return err
	}
	unlockMethod := data.Get("unlock_method").(string)

	workspaceListURL, err := b.parseUrl(ctx, "workspace_list_address", true, "", "")
	if err != nil {
		return err
	}

	headers := map[string]string{}
	rawHeaders := data.Get("headers").(map[string]interface{})
	if rawHeaders != nil {
		for k, v := range rawHeaders {
			headers[k] = v.(string)
		}
	}

	client := cleanhttp.DefaultPooledClient()

	if data.Get("skip_cert_verification").(bool) {
		// ignores TLS verification
		client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}

	rClient := retryablehttp.NewClient()
	rClient.HTTPClient = client
	rClient.RetryMax = data.Get("retry_max").(int)
	rClient.RetryWaitMin = time.Duration(data.Get("retry_wait_min").(int)) * time.Second
	rClient.RetryWaitMax = time.Duration(data.Get("retry_wait_max").(int)) * time.Second

	b.client = &httpClient{
		URL:          updateURL,
		Headers:      headers,
		UpdateMethod: updateMethod,

		LockURL:      lockURL,
		LockMethod:   lockMethod,
		UnlockURL:    unlockURL,
		UnlockMethod: unlockMethod,

		WorkspaceListURL: workspaceListURL,

		Username: data.Get("username").(string),
		Password: data.Get("password").(string),

		// accessible only for testing use
		Client: rClient,
	}
	b.workspacePathElement = data.Get("workspace_path_element").(string)
	return nil
}

func (b *Backend) StateMgr(name string) (state.State, error) {
	// workspace enabled
	if b.workspacePathElement != "" {
		updateURL, err := b.parseUrl(b.ctx, "address", false, b.workspacePathElement, name)
		if err != nil {
			return nil, err
		}

		lockURL, err := b.parseUrl(b.ctx, "lock_address", true, b.workspacePathElement, name)
		if err != nil {
			return nil, err
		}

		unlockURL, err := b.parseUrl(b.ctx, "unlock_address", true, b.workspacePathElement, name)
		if err != nil {
			return nil, err
		}

		b.client.URL = updateURL
		b.client.LockURL = lockURL
		b.client.UnlockURL = unlockURL
	} else {
		if name != backend.DefaultStateName {
			return nil, backend.ErrWorkspacesNotSupported
		}
	}

	return &remote.State{Client: b.client}, nil
}

func (b *Backend) Workspaces() ([]string, error) {
	if b.client.WorkspaceListURL != nil {
		return b.client.WorkspaceList()
	}
	return nil, backend.ErrWorkspacesNotSupported
}

func (b *Backend) DeleteWorkspace(string) error {
	return backend.ErrWorkspacesNotSupported
}
