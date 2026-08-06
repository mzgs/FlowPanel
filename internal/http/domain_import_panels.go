package httpx

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

const panelDiscoveryTimeout = 30 * time.Second

type panelConnectionInput struct {
	Provider  string `json:"provider"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	Secret    string `json:"secret"`
	AuthType  string `json:"auth_type"`
	VerifyTLS bool   `json:"verify_tls"`
}

type remotePanelSite struct {
	ID             string `json:"id"`
	SubscriptionID string `json:"subscription_id,omitempty"`
	Hostname       string `json:"hostname"`
	DocumentRoot   string `json:"document_root"`
	FTPUsername    string `json:"ftp_username,omitempty"`
}

type remotePanelDatabase struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	SiteID   string `json:"site_id,omitempty"`
	Type     string `json:"type"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Username string `json:"username,omitempty"`
}

type panelDiscoveryResult struct {
	Sites     []remotePanelSite     `json:"sites"`
	Databases []remotePanelDatabase `json:"databases"`
}

func (input *panelConnectionInput) normalizeAndValidate() map[string]string {
	input.Provider = strings.ToLower(strings.TrimSpace(input.Provider))
	input.Host = strings.Trim(strings.TrimSpace(input.Host), "[]")
	input.Username = strings.TrimSpace(input.Username)
	input.AuthType = strings.ToLower(strings.TrimSpace(input.AuthType))
	validation := map[string]string{}

	if input.Provider != "cpanel" && input.Provider != "plesk" {
		validation["provider"] = "Select cPanel or Plesk."
	}
	if input.Host == "" || strings.ContainsAny(input.Host, "/?#@") {
		validation["host"] = "Enter a valid panel hostname or IP address."
	}
	if input.Port < 1 || input.Port > 65535 {
		validation["port"] = "Port must be between 1 and 65535."
	}
	if input.AuthType != "token" && input.AuthType != "password" {
		validation["auth_type"] = "Select API token or password authentication."
	}
	if input.AuthType == "password" || input.Provider == "cpanel" {
		if input.Username == "" {
			validation["username"] = "Enter the panel username."
		}
	}
	if strings.TrimSpace(input.Secret) == "" {
		validation["secret"] = "Enter the API token or panel password."
	}

	return validation
}

func discoverPanel(ctx context.Context, input panelConnectionInput) (panelDiscoveryResult, error) {
	client := &http.Client{
		Timeout: panelDiscoveryTimeout,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: !input.VerifyTLS, // User-controlled for panels with self-signed certificates.
		}},
	}
	if input.Provider == "cpanel" {
		return discoverCPanel(ctx, client, input)
	}
	return discoverPlesk(ctx, client, input)
}

type cPanelResponse struct {
	Result struct {
		Data     json.RawMessage `json:"data"`
		Errors   any             `json:"errors"`
		Messages any             `json:"messages"`
		Status   int             `json:"status"`
	} `json:"result"`
}

func discoverCPanel(ctx context.Context, client *http.Client, input panelConnectionInput) (panelDiscoveryResult, error) {
	data, err := callCPanel(ctx, client, input, "DomainInfo/list_domains", nil)
	if err != nil {
		return panelDiscoveryResult{}, err
	}
	var domains struct {
		Main   string   `json:"main_domain"`
		Addon  []string `json:"addon_domains"`
		Sub    []string `json:"sub_domains"`
		Parked []string `json:"parked_domains"`
	}
	if err := json.Unmarshal(data, &domains); err != nil {
		return panelDiscoveryResult{}, fmt.Errorf("decode cPanel domains: %w", err)
	}

	hostnames := append([]string{domains.Main}, domains.Addon...)
	hostnames = append(hostnames, domains.Sub...)
	seen := map[string]struct{}{}
	sites := make([]remotePanelSite, 0, len(hostnames))
	for _, hostname := range hostnames {
		hostname = strings.TrimSpace(hostname)
		if hostname == "" {
			continue
		}
		if _, exists := seen[hostname]; exists {
			continue
		}
		seen[hostname] = struct{}{}

		domainData, requestErr := callCPanel(ctx, client, input, "DomainInfo/single_domain_data", url.Values{"domain": {hostname}})
		if requestErr != nil {
			return panelDiscoveryResult{}, requestErr
		}
		var details struct {
			DocumentRoot string `json:"documentroot"`
			HomeDir      string `json:"homedir"`
		}
		if err := json.Unmarshal(domainData, &details); err != nil {
			return panelDiscoveryResult{}, fmt.Errorf("decode cPanel domain %q: %w", hostname, err)
		}
		documentRoot := cPanelFTPPath(details.DocumentRoot, details.HomeDir, input.Username)
		if documentRoot == "" && hostname == domains.Main {
			documentRoot = "public_html"
		}
		sites = append(sites, remotePanelSite{
			ID:           hostname,
			Hostname:     hostname,
			DocumentRoot: documentRoot,
			FTPUsername:  input.Username,
		})
	}

	databaseData, err := callCPanel(ctx, client, input, "Mysql/list_databases", nil)
	if err != nil {
		return panelDiscoveryResult{}, err
	}
	var databaseRecords []struct {
		Database string          `json:"database"`
		Users    json.RawMessage `json:"users"`
	}
	if err := json.Unmarshal(databaseData, &databaseRecords); err != nil {
		return panelDiscoveryResult{}, fmt.Errorf("decode cPanel databases: %w", err)
	}
	databases := make([]remotePanelDatabase, 0, len(databaseRecords))
	for _, record := range databaseRecords {
		name := strings.TrimSpace(record.Database)
		if name == "" {
			continue
		}
		username := firstCPanelDatabaseUser(record.Users)
		databases = append(databases, remotePanelDatabase{
			ID:       name,
			Name:     name,
			Type:     "mysql",
			Host:     input.Host,
			Port:     3306,
			Username: username,
		})
	}
	sortPanelDiscovery(sites, databases)
	return panelDiscoveryResult{Sites: sites, Databases: databases}, nil
}

func callCPanel(ctx context.Context, client *http.Client, input panelConnectionInput, operation string, query url.Values) (json.RawMessage, error) {
	endpoint := url.URL{
		Scheme:   "https",
		Host:     net.JoinHostPort(input.Host, strconv.Itoa(input.Port)),
		Path:     path.Join("/execute", operation),
		RawQuery: query.Encode(),
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	if input.AuthType == "token" {
		req.Header.Set("Authorization", "cpanel "+input.Username+":"+input.Secret)
	} else {
		req.SetBasicAuth(input.Username, input.Secret)
	}

	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to cPanel API: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read cPanel API response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("cPanel API returned %s", response.Status)
	}
	var payload cPanelResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode cPanel API response: %w", err)
	}
	if payload.Result.Status != 1 {
		message := firstPanelMessage(payload.Result.Errors, payload.Result.Messages)
		if message == "" {
			message = "cPanel rejected the request"
		}
		return nil, errors.New(message)
	}
	return payload.Result.Data, nil
}

func cPanelFTPPath(documentRoot, homeDir, username string) string {
	documentRoot = path.Clean(strings.ReplaceAll(strings.TrimSpace(documentRoot), "\\", "/"))
	homeDir = strings.TrimSuffix(path.Clean(strings.ReplaceAll(strings.TrimSpace(homeDir), "\\", "/")), "/")
	for _, prefix := range []string{homeDir, "/home/" + username, "/home2/" + username, "/home3/" + username} {
		if prefix != "" && prefix != "." && strings.HasPrefix(documentRoot, prefix+"/") {
			return strings.TrimPrefix(documentRoot, prefix+"/")
		}
	}
	if !strings.HasPrefix(documentRoot, "/") && documentRoot != "." {
		return documentRoot
	}
	return ""
}

type pleskPacket struct {
	Webspace struct {
		Results []pleskWebspaceResult `xml:"result"`
	} `xml:"webspace>get"`
	Databases struct {
		Results []pleskDatabaseResult `xml:"result"`
	} `xml:"database>get-db"`
	Sites struct {
		Results []pleskSiteResult `xml:"result"`
	} `xml:"site>get"`
}

type pleskWebspaceResult struct {
	Status  string `xml:"status"`
	Error   string `xml:"errtext"`
	ID      string `xml:"id"`
	GenInfo struct {
		Name string `xml:"name"`
	} `xml:"data>gen_info"`
	Properties []struct {
		Name  string `xml:"name"`
		Value string `xml:"value"`
	} `xml:"data>hosting>vrt_hst>property"`
}

type pleskDatabaseResult struct {
	Status     string `xml:"status"`
	Error      string `xml:"errtext"`
	ID         string `xml:"id"`
	Name       string `xml:"name"`
	Type       string `xml:"type"`
	WebspaceID string `xml:"webspace-id"`
}

type pleskSiteResult struct {
	Status  string `xml:"status"`
	Error   string `xml:"errtext"`
	ID      string `xml:"id"`
	GenInfo struct {
		Name       string `xml:"name"`
		WebspaceID string `xml:"webspace-id"`
	} `xml:"data>gen_info"`
	Properties []struct {
		Name  string `xml:"name"`
		Value string `xml:"value"`
	} `xml:"data>hosting>vrt_hst>property"`
}

func discoverPlesk(ctx context.Context, client *http.Client, input panelConnectionInput) (panelDiscoveryResult, error) {
	requestXML := []byte(`<?xml version="1.0" encoding="UTF-8"?><packet version="1.6.9.1"><webspace><get><filter/><dataset><gen_info/><hosting/></dataset></get></webspace><site><get><filter/><dataset><gen_info/><hosting/></dataset></get></site><database><get-db><filter/></get-db></database></packet>`)
	endpoint := "https://" + net.JoinHostPort(input.Host, strconv.Itoa(input.Port)) + "/enterprise/control/agent.php"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestXML))
	if err != nil {
		return panelDiscoveryResult{}, err
	}
	req.Header.Set("Content-Type", "text/xml")
	if input.AuthType == "token" {
		req.Header.Set("KEY", input.Secret)
	} else {
		req.Header.Set("HTTP_AUTH_LOGIN", input.Username)
		req.Header.Set("HTTP_AUTH_PASSWD", input.Secret)
	}

	response, err := client.Do(req)
	if err != nil {
		return panelDiscoveryResult{}, fmt.Errorf("connect to Plesk API: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return panelDiscoveryResult{}, fmt.Errorf("read Plesk API response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return panelDiscoveryResult{}, fmt.Errorf("Plesk API returned %s", response.Status)
	}
	var packet pleskPacket
	if err := xml.Unmarshal(body, &packet); err != nil {
		return panelDiscoveryResult{}, fmt.Errorf("decode Plesk API response: %w", err)
	}

	sites := make([]remotePanelSite, 0, len(packet.Webspace.Results)+len(packet.Sites.Results))
	subscriptions := map[string]string{}
	subscriptionFTPUsers := map[string]string{}
	for _, result := range packet.Webspace.Results {
		if result.Status != "ok" {
			if result.Error != "" {
				return panelDiscoveryResult{}, errors.New(result.Error)
			}
			continue
		}
		properties := map[string]string{}
		for _, property := range result.Properties {
			properties[property.Name] = property.Value
		}
		hostname := strings.TrimSpace(result.GenInfo.Name)
		if hostname == "" {
			continue
		}
		subscriptionID := strings.TrimSpace(result.ID)
		subscriptions[subscriptionID] = hostname
		subscriptionFTPUsers[subscriptionID] = strings.TrimSpace(properties["ftp_login"])
		sites = append(sites, remotePanelSite{
			ID:             "webspace:" + subscriptionID,
			SubscriptionID: subscriptionID,
			Hostname:       hostname,
			DocumentRoot:   pleskFTPPath(properties["www_root"], hostname),
			FTPUsername:    strings.TrimSpace(properties["ftp_login"]),
		})
	}
	for _, result := range packet.Sites.Results {
		if result.Status != "ok" {
			continue
		}
		properties := map[string]string{}
		for _, property := range result.Properties {
			properties[property.Name] = property.Value
		}
		hostname := strings.TrimSpace(result.GenInfo.Name)
		subscriptionID := strings.TrimSpace(result.GenInfo.WebspaceID)
		if hostname == "" {
			continue
		}
		sites = append(sites, remotePanelSite{
			ID:             "site:" + strings.TrimSpace(result.ID),
			SubscriptionID: subscriptionID,
			Hostname:       hostname,
			DocumentRoot:   pleskFTPPath(properties["www_root"], subscriptions[subscriptionID]),
			FTPUsername:    firstNonEmptyString(strings.TrimSpace(properties["ftp_login"]), subscriptionFTPUsers[subscriptionID]),
		})
	}

	databases := make([]remotePanelDatabase, 0, len(packet.Databases.Results))
	for _, result := range packet.Databases.Results {
		if result.Status != "ok" {
			continue
		}
		name := strings.TrimSpace(result.Name)
		if name == "" {
			continue
		}
		databases = append(databases, remotePanelDatabase{
			ID:     strings.TrimSpace(result.ID),
			Name:   name,
			SiteID: strings.TrimSpace(result.WebspaceID),
			Type:   strings.ToLower(strings.TrimSpace(result.Type)),
			Host:   input.Host,
			Port:   3306,
		})
	}
	if len(sites) == 0 {
		return panelDiscoveryResult{}, errors.New("Plesk did not return any accessible websites")
	}
	sortPanelDiscovery(sites, databases)
	return panelDiscoveryResult{Sites: sites, Databases: databases}, nil
}

func pleskFTPPath(documentRoot, hostname string) string {
	documentRoot = path.Clean(strings.ReplaceAll(strings.TrimSpace(documentRoot), "\\", "/"))
	prefix := "/var/www/vhosts/" + hostname + "/"
	if strings.HasPrefix(documentRoot, prefix) {
		return strings.TrimPrefix(documentRoot, prefix)
	}
	if !strings.HasPrefix(documentRoot, "/") && documentRoot != "." {
		return documentRoot
	}
	return "httpdocs"
}

func sortPanelDiscovery(sites []remotePanelSite, databases []remotePanelDatabase) {
	sort.Slice(sites, func(i, j int) bool { return sites[i].Hostname < sites[j].Hostname })
	sort.Slice(databases, func(i, j int) bool { return databases[i].Name < databases[j].Name })
}

func firstPanelMessage(values ...any) string {
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case []any:
			for _, item := range typed {
				if message, ok := item.(string); ok && strings.TrimSpace(message) != "" {
					return strings.TrimSpace(message)
				}
			}
		}
	}
	return ""
}

func firstCPanelDatabaseUser(raw json.RawMessage) string {
	var names []string
	if json.Unmarshal(raw, &names) == nil && len(names) > 0 {
		return strings.TrimSpace(names[0])
	}
	var records []struct {
		Name string `json:"name"`
		User string `json:"user"`
	}
	if json.Unmarshal(raw, &records) == nil && len(records) > 0 {
		if strings.TrimSpace(records[0].Name) != "" {
			return strings.TrimSpace(records[0].Name)
		}
		return strings.TrimSpace(records[0].User)
	}
	return ""
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
