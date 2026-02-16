package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

const (
	googleAuthURL   = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL  = "https://oauth2.googleapis.com/token"
	googleDeviceURL = "https://oauth2.googleapis.com/device/code"
	gcpCredsURL     = "https://console.cloud.google.com/apis/credentials"
	gcpConsentURL   = "https://console.cloud.google.com/apis/credentials/consent"
	gcpChatAPIURL   = "https://console.cloud.google.com/apis/library/chat.googleapis.com"
)

var defaultChatScopes = []string{
	"https://www.googleapis.com/auth/chat.messages",
	"https://www.googleapis.com/auth/chat.spaces.readonly",
	"https://www.googleapis.com/auth/chat.memberships.readonly",
	"https://www.googleapis.com/auth/chat.users.readstate.readonly",
}

const defaultChatScopesCSV = "https://www.googleapis.com/auth/chat.messages,https://www.googleapis.com/auth/chat.spaces.readonly,https://www.googleapis.com/auth/chat.memberships.readonly,https://www.googleapis.com/auth/chat.users.readstate.readonly"

type OAuthClient struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

type AppConfig struct {
	DefaultProfile string      `json:"default_profile"`
	OAuthClient    OAuthClient `json:"oauth_client"`
	Scopes         []string    `json:"scopes"`
}

type StoredToken struct {
	Token   oauth2.Token `json:"token"`
	Scopes  []string     `json:"scopes"`
	Mode    string       `json:"mode"`
	SavedAt time.Time    `json:"saved_at"`
}

type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURL         string `json:"verification_url"`
	VerificationURLComplete string `json:"verification_url_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type DeviceTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
}

type ChatSpace struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	SpaceType   string `json:"spaceType"`
}

type ListSpacesResponse struct {
	Spaces        []ChatSpace `json:"spaces"`
	NextPageToken string      `json:"nextPageToken"`
}

type SpaceReadState struct {
	Name         string `json:"name"`
	LastReadTime string `json:"lastReadTime"`
}

type ChatSender struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type ChatMessage struct {
	Name       string     `json:"name"`
	CreateTime string     `json:"createTime"`
	Text       string     `json:"text"`
	Sender     ChatSender `json:"sender"`
}

type ListMessagesResponse struct {
	Messages      []ChatMessage `json:"messages"`
	NextPageToken string        `json:"nextPageToken"`
}

type PolledMessage struct {
	Space      string `json:"space"`
	Name       string `json:"name"`
	CreateTime string `json:"create_time"`
	Sender     string `json:"sender"`
	SenderUser string `json:"sender_user"`
	Text       string `json:"text"`
}

type ChatUser struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Type        string `json:"type"`
}

type ChatMembership struct {
	Name   string   `json:"name"`
	Member ChatUser `json:"member"`
}

type ListMembershipsResponse struct {
	Memberships   []ChatMembership `json:"memberships"`
	NextPageToken string           `json:"nextPageToken"`
}

type ChatUserResource struct {
	Name string `json:"name"`
}

type DMSpaceView struct {
	Space           string `json:"space"`
	PeerUser        string `json:"peer_user"`
	PeerDisplayName string `json:"peer_display_name,omitempty"`
}

type UnreadSpaceView struct {
	Space     string `json:"space"`
	SpaceType string `json:"space_type,omitempty"`
	Display   string `json:"display,omitempty"`
	LastRead  string `json:"last_read_time,omitempty"`
	Latest    string `json:"latest_message_time,omitempty"`
	IsUnread  bool   `json:"is_unread"`
}

type GoogleAPIErrorEnvelope struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

type AliasConfig struct {
	Aliases   map[string]string `json:"aliases"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		printRootHelp()
		return nil
	}

	switch os.Args[1] {
	case "auth":
		return runAuth(os.Args[2:])
	case "chat":
		return runChat(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("gchatctl dev")
		return nil
	case "help", "--help", "-h":
		printRootHelp()
		return nil
	default:
		printRootHelp()
		return fmt.Errorf("unknown command %q", os.Args[1])
	}
}

func printRootHelp() {
	fmt.Println("gchatctl: Google Chat CLI for agents")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  auth setup   Open OAuth setup checklist")
	fmt.Println("  auth login   Authenticate and save OAuth tokens")
	fmt.Println("  auth status  Show auth status")
	fmt.Println("  auth logout  Remove saved token")
	fmt.Println("  chat spaces  List spaces")
	fmt.Println("  chat messages List messages")
	fmt.Println("  version      Show version")
}

func runAuth(args []string) error {
	if len(args) == 0 {
		printAuthHelp()
		return nil
	}

	switch args[0] {
	case "setup":
		return runAuthSetup(args[1:])
	case "login":
		return runAuthLogin(args[1:])
	case "status":
		return runAuthStatus(args[1:])
	case "logout":
		return runAuthLogout(args[1:])
	case "help", "--help", "-h":
		printAuthHelp()
		return nil
	default:
		printAuthHelp()
		return fmt.Errorf("unknown auth command %q", args[0])
	}
}

func printAuthHelp() {
	fmt.Println("gchatctl auth commands:")
	fmt.Println("  auth setup [--open]")
	fmt.Println("  auth login [--profile default] [--mode auto|browser|device] [--no-open] [--timeout 3m] [--all-scopes] [--client-id ...] [--client-secret optional] [--scopes comma,list]")
	fmt.Println("  auth status [--profile default] [--json]")
	fmt.Println("  auth logout [--profile default]")
}

func runChat(args []string) error {
	if len(args) == 0 {
		printChatHelp()
		return nil
	}

	switch args[0] {
	case "dm":
		return runChatDM(args[1:])
	case "spaces":
		return runChatSpaces(args[1:])
	case "messages":
		return runChatMessages(args[1:])
	case "users":
		return runChatUsers(args[1:])
	case "help", "--help", "-h":
		printChatHelp()
		return nil
	default:
		printChatHelp()
		return fmt.Errorf("unknown chat command %q", args[0])
	}
}

func printChatHelp() {
	fmt.Println("gchatctl chat commands:")
	fmt.Println("  chat dm find (--email user@company.com | --user users/...) [--profile default] [--json]")
	fmt.Println("  chat spaces list [--profile default] [--limit 100] [--json]")
	fmt.Println("  chat spaces unread [--profile default] [--limit 100] [--json]")
	fmt.Println("  chat spaces dm [--profile default] [--limit 100] [--json]")
	fmt.Println("  chat spaces members --space spaces/... [--profile default] [--json]")
	fmt.Println("  chat messages list --space spaces/AAA... [--profile default] [--limit 50] [--json]")
	fmt.Println("  chat messages send (--space spaces/AAA... | --email user@company.com | --user users/...) --text \"...\" [--profile default] [--json]")
	fmt.Println("  chat messages with (--email user@company.com | --user users/...) [--profile default] [--limit 10] [--json]")
	fmt.Println("  chat messages senders --space spaces/AAA... [--profile default] [--limit 5] [--json]")
	fmt.Println("  chat messages poll [--profile default] [--space spaces/AAA...] [--since 5m] [--interval 30s] [--iterations 1] [--limit 100] [--json]")
	fmt.Println("  chat users aliases list [--json]")
	fmt.Println("  chat users aliases set --user users/... --name \"Display Name\"")
	fmt.Println("  chat users aliases set-from-space --profile work --space spaces/... --name \"Simon\"")
	fmt.Println("  chat users aliases infer --profile work [--apply]")
	fmt.Println("  chat users aliases unset --user users/...")
}

func runChatDM(args []string) error {
	if len(args) == 0 {
		printChatDMHelp()
		return nil
	}
	switch args[0] {
	case "find":
		return runChatDMFind(args[1:])
	case "help", "--help", "-h":
		printChatDMHelp()
		return nil
	default:
		printChatDMHelp()
		return fmt.Errorf("unknown chat dm command %q", args[0])
	}
}

func printChatDMHelp() {
	fmt.Println("gchatctl chat dm commands:")
	fmt.Println("  chat dm find (--email user@company.com | --user users/...) [--profile default] [--json]")
}

func runChatDMFind(args []string) error {
	fs := flag.NewFlagSet("chat dm find", flag.ContinueOnError)
	profile := fs.String("profile", "", "profile name")
	email := fs.String("email", "", "user email (maps to users/<email>)")
	user := fs.String("user", "", "user resource name (users/...)")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*email) == "" && strings.TrimSpace(*user) == "" {
		return errors.New("one of --email or --user is required")
	}
	if strings.TrimSpace(*email) != "" && strings.TrimSpace(*user) != "" {
		return errors.New("use either --email or --user, not both")
	}

	targetUser := normalizeUserRef(firstNonEmpty(*user, *email))
	ctx := context.Background()
	selectedProfile, cfg, st, err := loadAuthContext(*profile)
	if err != nil {
		return err
	}
	oauthCfg := oauthConfigFrom(cfg, st.Scopes)
	tokenSource := oauthCfg.TokenSource(ctx, &st.Token)
	client := oauth2.NewClient(ctx, tokenSource)

	space, err := findDirectMessageSpace(ctx, client, targetUser)
	if err != nil {
		return err
	}
	if err := saveRefreshedTokenIfChanged(selectedProfile, st, tokenSource); err != nil {
		return err
	}
	if *jsonOut {
		b, _ := json.MarshalIndent(map[string]any{
			"profile": selectedProfile,
			"target":  targetUser,
			"space":   space,
		}, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("Direct message with %s: %s\n", targetUser, space.Name)
	return nil
}

func runChatSpaces(args []string) error {
	if len(args) == 0 {
		printChatSpacesHelp()
		return nil
	}
	switch args[0] {
	case "list":
		return runChatSpacesList(args[1:])
	case "unread":
		return runChatSpacesUnread(args[1:])
	case "dm":
		return runChatSpacesDM(args[1:])
	case "members":
		return runChatSpacesMembers(args[1:])
	case "help", "--help", "-h":
		printChatSpacesHelp()
		return nil
	default:
		printChatSpacesHelp()
		return fmt.Errorf("unknown chat spaces command %q", args[0])
	}
}

func printChatSpacesHelp() {
	fmt.Println("gchatctl chat spaces commands:")
	fmt.Println("  chat spaces list [--profile default] [--limit 100] [--json]")
	fmt.Println("  chat spaces unread [--profile default] [--limit 100] [--json]")
	fmt.Println("  chat spaces dm [--profile default] [--limit 100] [--json]")
	fmt.Println("  chat spaces members --space spaces/... [--profile default] [--json]")
}

func runChatSpacesList(args []string) error {
	fs := flag.NewFlagSet("chat spaces list", flag.ContinueOnError)
	profile := fs.String("profile", "", "profile name")
	limit := fs.Int("limit", 100, "max spaces to return")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit <= 0 {
		return errors.New("--limit must be greater than 0")
	}

	ctx := context.Background()
	selectedProfile, cfg, st, err := loadAuthContext(*profile)
	if err != nil {
		return err
	}

	oauthCfg := oauthConfigFrom(cfg, st.Scopes)
	tokenSource := oauthCfg.TokenSource(ctx, &st.Token)
	client := oauth2.NewClient(ctx, tokenSource)

	items, err := listSpaces(ctx, client, *limit)
	if err != nil {
		return err
	}
	if err := saveRefreshedTokenIfChanged(selectedProfile, st, tokenSource); err != nil {
		return err
	}

	if *jsonOut {
		out := map[string]any{
			"profile": selectedProfile,
			"count":   len(items),
			"spaces":  items,
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
		return nil
	}

	if len(items) == 0 {
		fmt.Printf("No spaces found for profile %q\n", selectedProfile)
		return nil
	}
	fmt.Printf("Spaces (%d) for profile %q:\n", len(items), selectedProfile)
	for _, s := range items {
		display := firstNonEmpty(strings.TrimSpace(s.DisplayName), "(no display name)")
		fmt.Printf("- %s  [%s]  %s\n", s.Name, firstNonEmpty(s.SpaceType, "SPACE"), display)
	}
	return nil
}

func runChatSpacesUnread(args []string) error {
	fs := flag.NewFlagSet("chat spaces unread", flag.ContinueOnError)
	profile := fs.String("profile", "", "profile name")
	limit := fs.Int("limit", 100, "max spaces to check")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit <= 0 {
		return errors.New("--limit must be greater than 0")
	}

	ctx := context.Background()
	selectedProfile, cfg, st, err := loadAuthContext(*profile)
	if err != nil {
		return err
	}
	oauthCfg := oauthConfigFrom(cfg, st.Scopes)
	tokenSource := oauthCfg.TokenSource(ctx, &st.Token)
	client := oauth2.NewClient(ctx, tokenSource)

	spaces, err := listSpaces(ctx, client, *limit)
	if err != nil {
		return err
	}

	unread := make([]UnreadSpaceView, 0, minInt(32, len(spaces)))
	for _, s := range spaces {
		latestMsg, lerr := listMessages(ctx, client, s.Name, 1)
		if lerr != nil || len(latestMsg) == 0 {
			continue
		}
		latestTS, lok := parseMessageTime(latestMsg[0].CreateTime)
		if !lok {
			continue
		}
		rs, rerr := getSpaceReadState(ctx, client, s.Name)
		if rerr != nil {
			return rerr
		}
		lastReadTS, rok := parseMessageTime(rs.LastReadTime)
		isUnread := !rok || latestTS.After(lastReadTS)
		if !isUnread {
			continue
		}
		unread = append(unread, UnreadSpaceView{
			Space:     s.Name,
			SpaceType: s.SpaceType,
			Display:   strings.TrimSpace(s.DisplayName),
			LastRead:  rs.LastReadTime,
			Latest:    latestMsg[0].CreateTime,
			IsUnread:  true,
		})
	}

	sort.Slice(unread, func(i, j int) bool {
		ti, _ := parseMessageTime(unread[i].Latest)
		tj, _ := parseMessageTime(unread[j].Latest)
		return tj.Before(ti)
	})

	if err := saveRefreshedTokenIfChanged(selectedProfile, st, tokenSource); err != nil {
		return err
	}

	if *jsonOut {
		out := map[string]any{
			"profile": selectedProfile,
			"count":   len(unread),
			"spaces":  unread,
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	if len(unread) == 0 {
		fmt.Printf("No unread spaces for profile %q\n", selectedProfile)
		return nil
	}
	fmt.Printf("Unread spaces (%d) for profile %q:\n", len(unread), selectedProfile)
	for _, u := range unread {
		label := firstNonEmpty(strings.TrimSpace(u.Display), "(no display name)")
		fmt.Printf("- %s  [%s]  %s  latest=%s\n", u.Space, firstNonEmpty(u.SpaceType, "SPACE"), label, u.Latest)
	}
	return nil
}

func runChatSpacesDM(args []string) error {
	fs := flag.NewFlagSet("chat spaces dm", flag.ContinueOnError)
	profile := fs.String("profile", "", "profile name")
	limit := fs.Int("limit", 100, "max DM spaces to return")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit <= 0 {
		return errors.New("--limit must be greater than 0")
	}

	ctx := context.Background()
	selectedProfile, cfg, st, err := loadAuthContext(*profile)
	if err != nil {
		return err
	}
	oauthCfg := oauthConfigFrom(cfg, st.Scopes)
	tokenSource := oauthCfg.TokenSource(ctx, &st.Token)
	client := oauth2.NewClient(ctx, tokenSource)

	spaces, err := listSpaces(ctx, client, *limit*2)
	if err != nil {
		return err
	}
	aliases, _ := loadAliases()
	me, _ := currentUserRef(ctx, client)
	if strings.TrimSpace(me) == "" {
		me = inferCurrentUserFromDMS(ctx, client, spaces)
	}

	out := make([]DMSpaceView, 0, *limit)
	for _, s := range spaces {
		if s.SpaceType != "DIRECT_MESSAGE" {
			continue
		}
		peerUser, peerName, err := dmPeerForSpace(ctx, client, s.Name, me)
		if err != nil {
			continue
		}
		if peerUser == "" {
			continue
		}
		if peerName == "" {
			peerName = strings.TrimSpace(aliases[normalizeUserRef(peerUser)])
		}
		out = append(out, DMSpaceView{
			Space:           s.Name,
			PeerUser:        normalizeUserRef(peerUser),
			PeerDisplayName: peerName,
		})
		if len(out) >= *limit {
			break
		}
	}

	if err := saveRefreshedTokenIfChanged(selectedProfile, st, tokenSource); err != nil {
		return err
	}

	if *jsonOut {
		b, _ := json.MarshalIndent(map[string]any{
			"profile": selectedProfile,
			"count":   len(out),
			"dms":     out,
		}, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	if len(out) == 0 {
		fmt.Printf("No direct-message spaces found for profile %q\n", selectedProfile)
		return nil
	}
	fmt.Printf("Direct-message spaces (%d) for profile %q:\n", len(out), selectedProfile)
	for _, dm := range out {
		label := firstNonEmpty(dm.PeerDisplayName, dm.PeerUser)
		fmt.Printf("- %s  peer=%s (%s)\n", dm.Space, label, dm.PeerUser)
	}
	return nil
}

func runChatSpacesMembers(args []string) error {
	fs := flag.NewFlagSet("chat spaces members", flag.ContinueOnError)
	profile := fs.String("profile", "", "profile name")
	space := fs.String("space", "", "space resource name or ID")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*space) == "" {
		return errors.New("--space is required")
	}
	spaceName := normalizeSpaceName(*space)

	ctx := context.Background()
	selectedProfile, cfg, st, err := loadAuthContext(*profile)
	if err != nil {
		return err
	}
	oauthCfg := oauthConfigFrom(cfg, st.Scopes)
	tokenSource := oauthCfg.TokenSource(ctx, &st.Token)
	client := oauth2.NewClient(ctx, tokenSource)

	aliases, _ := loadAliases()
	members, err := listSpaceMembers(ctx, client, spaceName)
	if err != nil {
		return err
	}
	if err := saveRefreshedTokenIfChanged(selectedProfile, st, tokenSource); err != nil {
		return err
	}

	type memberOut struct {
		User        string `json:"user"`
		DisplayName string `json:"display_name,omitempty"`
		Type        string `json:"type,omitempty"`
		Alias       string `json:"alias,omitempty"`
	}
	out := make([]memberOut, 0, len(members))
	for _, m := range members {
		u := normalizeUserRef(m.Member.Name)
		out = append(out, memberOut{
			User:        u,
			DisplayName: strings.TrimSpace(m.Member.DisplayName),
			Type:        strings.TrimSpace(m.Member.Type),
			Alias:       strings.TrimSpace(aliases[u]),
		})
	}

	if *jsonOut {
		b, _ := json.MarshalIndent(map[string]any{
			"profile": selectedProfile,
			"space":   spaceName,
			"count":   len(out),
			"members": out,
		}, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	if len(out) == 0 {
		fmt.Printf("No members found in %s\n", spaceName)
		return nil
	}
	fmt.Printf("Members in %s:\n", spaceName)
	for _, m := range out {
		label := firstNonEmpty(m.Alias, m.DisplayName, m.User)
		fmt.Printf("- %s  (%s)\n", label, m.User)
	}
	return nil
}

func runChatMessages(args []string) error {
	if len(args) == 0 {
		printChatMessagesHelp()
		return nil
	}
	switch args[0] {
	case "list":
		return runChatMessagesList(args[1:])
	case "send":
		return runChatMessagesSend(args[1:])
	case "with":
		return runChatMessagesWith(args[1:])
	case "senders":
		return runChatMessagesSenders(args[1:])
	case "poll":
		return runChatMessagesPoll(args[1:])
	case "help", "--help", "-h":
		printChatMessagesHelp()
		return nil
	default:
		printChatMessagesHelp()
		return fmt.Errorf("unknown chat messages command %q", args[0])
	}
}

func runChatUsers(args []string) error {
	if len(args) == 0 {
		printChatUsersHelp()
		return nil
	}
	switch args[0] {
	case "aliases":
		return runChatUsersAliases(args[1:])
	case "help", "--help", "-h":
		printChatUsersHelp()
		return nil
	default:
		printChatUsersHelp()
		return fmt.Errorf("unknown chat users command %q", args[0])
	}
}

func printChatUsersHelp() {
	fmt.Println("gchatctl chat users commands:")
	fmt.Println("  chat users aliases list [--json]")
	fmt.Println("  chat users aliases set --user users/... --name \"Display Name\"")
	fmt.Println("  chat users aliases set-from-space --profile work --space spaces/... --name \"Simon\"")
	fmt.Println("  chat users aliases infer --profile work [--apply]")
	fmt.Println("  chat users aliases unset --user users/...")
}

func runChatUsersAliases(args []string) error {
	if len(args) == 0 {
		printChatUsersHelp()
		return nil
	}
	switch args[0] {
	case "list":
		return runChatUsersAliasesList(args[1:])
	case "set":
		return runChatUsersAliasesSet(args[1:])
	case "set-from-space":
		return runChatUsersAliasesSetFromSpace(args[1:])
	case "infer":
		return runChatUsersAliasesInfer(args[1:])
	case "unset":
		return runChatUsersAliasesUnset(args[1:])
	case "help", "--help", "-h":
		printChatUsersHelp()
		return nil
	default:
		printChatUsersHelp()
		return fmt.Errorf("unknown chat users aliases command %q", args[0])
	}
}

func runChatUsersAliasesList(args []string) error {
	fs := flag.NewFlagSet("chat users aliases list", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	aliases, err := loadAliases()
	if err != nil {
		return err
	}
	if *jsonOut {
		out := map[string]any{
			"count":   len(aliases),
			"aliases": aliases,
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	if len(aliases) == 0 {
		fmt.Println("No user aliases configured")
		return nil
	}
	fmt.Printf("User aliases (%d):\n", len(aliases))
	for user, name := range aliases {
		fmt.Printf("- %s => %s\n", user, name)
	}
	return nil
}

func runChatUsersAliasesSet(args []string) error {
	fs := flag.NewFlagSet("chat users aliases set", flag.ContinueOnError)
	user := fs.String("user", "", "user resource name (users/...)")
	name := fs.String("name", "", "display name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*user) == "" {
		return errors.New("--user is required (example: users/123...)")
	}
	if strings.TrimSpace(*name) == "" {
		return errors.New("--name is required")
	}
	aliases, err := loadAliases()
	if err != nil {
		return err
	}
	key := normalizeUserRef(*user)
	aliases[key] = strings.TrimSpace(*name)
	if err := saveAliases(aliases); err != nil {
		return err
	}
	fmt.Printf("Saved alias: %s => %s\n", key, aliases[key])
	return nil
}

func runChatUsersAliasesUnset(args []string) error {
	fs := flag.NewFlagSet("chat users aliases unset", flag.ContinueOnError)
	user := fs.String("user", "", "user resource name (users/...)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*user) == "" {
		return errors.New("--user is required (example: users/123...)")
	}
	aliases, err := loadAliases()
	if err != nil {
		return err
	}
	key := normalizeUserRef(*user)
	delete(aliases, key)
	if err := saveAliases(aliases); err != nil {
		return err
	}
	fmt.Printf("Removed alias for %s\n", key)
	return nil
}

func runChatUsersAliasesSetFromSpace(args []string) error {
	fs := flag.NewFlagSet("chat users aliases set-from-space", flag.ContinueOnError)
	profile := fs.String("profile", "", "profile name")
	space := fs.String("space", "", "space resource name or ID (DIRECT_MESSAGE)")
	name := fs.String("name", "", "display name alias")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*space) == "" {
		return errors.New("--space is required")
	}
	if strings.TrimSpace(*name) == "" {
		return errors.New("--name is required")
	}
	spaceName := normalizeSpaceName(*space)

	ctx := context.Background()
	selectedProfile, cfg, st, err := loadAuthContext(*profile)
	if err != nil {
		return err
	}
	oauthCfg := oauthConfigFrom(cfg, st.Scopes)
	tokenSource := oauthCfg.TokenSource(ctx, &st.Token)
	client := oauth2.NewClient(ctx, tokenSource)

	me, _ := currentUserRef(ctx, client)
	if strings.TrimSpace(me) == "" {
		spaceList, _ := listSpaces(ctx, client, 200)
		me = inferCurrentUserFromDMS(ctx, client, spaceList)
	}
	peerUser, _, err := dmPeerForSpace(ctx, client, spaceName, me)
	if err != nil {
		return err
	}
	if strings.TrimSpace(peerUser) == "" {
		return errors.New("could not infer DM peer user from space memberships")
	}

	aliases, err := loadAliases()
	if err != nil {
		return err
	}
	key := normalizeUserRef(peerUser)
	aliases[key] = strings.TrimSpace(*name)
	if err := saveAliases(aliases); err != nil {
		return err
	}
	if err := saveRefreshedTokenIfChanged(selectedProfile, st, tokenSource); err != nil {
		return err
	}
	fmt.Printf("Saved alias from %s: %s => %s\n", spaceName, key, aliases[key])
	return nil
}

func runChatUsersAliasesInfer(args []string) error {
	fs := flag.NewFlagSet("chat users aliases infer", flag.ContinueOnError)
	profile := fs.String("profile", "", "profile name")
	spaceLimit := fs.Int("space-limit", 100, "max spaces to scan")
	messageLimit := fs.Int("message-limit", 100, "max messages per space")
	apply := fs.Bool("apply", false, "save inferred aliases")
	force := fs.Bool("force", false, "overwrite existing aliases")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *spaceLimit <= 0 || *messageLimit <= 0 {
		return errors.New("--space-limit and --message-limit must be greater than 0")
	}

	ctx := context.Background()
	selectedProfile, cfg, st, err := loadAuthContext(*profile)
	if err != nil {
		return err
	}
	oauthCfg := oauthConfigFrom(cfg, st.Scopes)
	tokenSource := oauthCfg.TokenSource(ctx, &st.Token)
	client := oauth2.NewClient(ctx, tokenSource)

	spaces, err := listSpaces(ctx, client, *spaceLimit)
	if err != nil {
		return err
	}
	existing, _ := loadAliases()
	aliasHits := map[string]map[string]int{}
	re := regexp.MustCompile(`(?i)^\s*([\p{L}][\p{L}\s.'-]{1,80}?)\s*\([^\s()]+@[^\s()]+\)`)

	for _, s := range spaces {
		members, err := listSpaceMembers(ctx, client, s.Name)
		if err != nil {
			continue
		}
		humanSenders := map[string]struct{}{}
		for _, mem := range members {
			if strings.ToUpper(strings.TrimSpace(mem.Member.Type)) != "HUMAN" {
				continue
			}
			id := normalizeUserRef(mem.Member.Name)
			if id == "" || id == "users/" {
				continue
			}
			humanSenders[id] = struct{}{}
		}
		if len(humanSenders) == 0 {
			continue
		}

		msgs, err := listMessages(ctx, client, s.Name, *messageLimit)
		if err != nil {
			continue
		}
		for _, m := range msgs {
			user := normalizeUserRef(m.Sender.Name)
			if user == "" || user == "users/" {
				continue
			}
			if _, ok := humanSenders[user]; !ok {
				continue
			}
			text := strings.TrimSpace(m.Text)
			if text == "" {
				continue
			}
			match := re.FindStringSubmatch(text)
			if len(match) < 2 {
				continue
			}
			name := strings.TrimSpace(match[1])
			if name == "" {
				continue
			}
			if _, ok := aliasHits[user]; !ok {
				aliasHits[user] = map[string]int{}
			}
			aliasHits[user][name]++
		}
	}

	type inferred struct {
		User     string `json:"user"`
		Name     string `json:"name"`
		Hits     int    `json:"hits"`
		Existing string `json:"existing,omitempty"`
		Applied  bool   `json:"applied"`
	}
	inferredList := make([]inferred, 0, len(aliasHits))
	for user, names := range aliasHits {
		bestName := ""
		bestHits := 0
		for n, c := range names {
			if c > bestHits {
				bestHits = c
				bestName = n
			}
		}
		if bestName == "" {
			continue
		}
		cur := strings.TrimSpace(existing[user])
		applied := false
		if *apply {
			if cur == "" || *force {
				existing[user] = bestName
				applied = true
			}
		}
		inferredList = append(inferredList, inferred{
			User:     user,
			Name:     bestName,
			Hits:     bestHits,
			Existing: cur,
			Applied:  applied,
		})
	}
	sort.Slice(inferredList, func(i, j int) bool {
		if inferredList[i].Hits == inferredList[j].Hits {
			return inferredList[i].User < inferredList[j].User
		}
		return inferredList[i].Hits > inferredList[j].Hits
	})

	if *apply {
		if err := saveAliases(existing); err != nil {
			return err
		}
	}
	if err := saveRefreshedTokenIfChanged(selectedProfile, st, tokenSource); err != nil {
		return err
	}

	if *jsonOut {
		b, _ := json.MarshalIndent(map[string]any{
			"profile":   selectedProfile,
			"count":     len(inferredList),
			"inferred":  inferredList,
			"applied":   *apply,
			"overwrote": *force,
		}, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	if len(inferredList) == 0 {
		fmt.Println("No aliases inferred from recent messages")
		return nil
	}
	fmt.Printf("Inferred aliases (%d):\n", len(inferredList))
	for _, it := range inferredList {
		status := "suggested"
		if it.Applied {
			status = "applied"
		}
		if it.Existing != "" && !it.Applied && it.Existing != it.Name {
			status = "kept-existing"
		}
		fmt.Printf("- %s => %s  (hits=%d, %s)\n", it.User, it.Name, it.Hits, status)
	}
	return nil
}

func printChatMessagesHelp() {
	fmt.Println("gchatctl chat messages commands:")
	fmt.Println("  chat messages list --space spaces/AAA... [--profile default] [--limit 50] [--json]")
	fmt.Println("  chat messages send (--space spaces/AAA... | --email user@company.com | --user users/...) --text \"...\" [--profile default] [--json]")
	fmt.Println("  chat messages with (--email user@company.com | --user users/...) [--profile default] [--limit 10] [--json]")
	fmt.Println("  chat messages senders --space spaces/AAA... [--profile default] [--limit 5] [--json]")
	fmt.Println("  chat messages poll [--profile default] [--space spaces/AAA...] [--since 5m] [--interval 30s] [--iterations 1] [--limit 100] [--json]")
}

func runChatMessagesList(args []string) error {
	fs := flag.NewFlagSet("chat messages list", flag.ContinueOnError)
	profile := fs.String("profile", "", "profile name")
	space := fs.String("space", "", "space resource name or ID")
	limit := fs.Int("limit", 50, "max messages to return")
	jsonOut := fs.Bool("json", false, "print JSON")
	person := fs.String("person", "", "filter by sender (display name, user ID, or users/...)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit <= 0 {
		return errors.New("--limit must be greater than 0")
	}
	if strings.TrimSpace(*space) == "" {
		return errors.New("--space is required (example: --space spaces/AAA...); for person chat use: gchatctl chat messages with --email user@company.com")
	}
	spaceName := normalizeSpaceName(*space)

	ctx := context.Background()
	selectedProfile, cfg, st, err := loadAuthContext(*profile)
	if err != nil {
		return err
	}

	oauthCfg := oauthConfigFrom(cfg, st.Scopes)
	tokenSource := oauthCfg.TokenSource(ctx, &st.Token)
	client := oauth2.NewClient(ctx, tokenSource)

	items, err := listMessages(ctx, client, spaceName, *limit)
	if err != nil {
		return err
	}
	aliases, _ := loadAliases()
	senderNames, nameErr := listSpaceSenderNames(ctx, client, spaceName)
	if nameErr != nil {
		// Keep message listing functional even if sender-name enrichment fails.
		senderNames = map[string]string{}
	}
	for i := range items {
		if strings.TrimSpace(items[i].Sender.DisplayName) == "" {
			if v := strings.TrimSpace(senderNames[items[i].Sender.Name]); v != "" {
				items[i].Sender.DisplayName = v
				continue
			}
			if v := strings.TrimSpace(aliases[normalizeUserRef(items[i].Sender.Name)]); v != "" {
				items[i].Sender.DisplayName = v
			}
		}
	}
	if strings.TrimSpace(*person) != "" {
		items = filterMessagesByPerson(items, *person)
	}
	if err := saveRefreshedTokenIfChanged(selectedProfile, st, tokenSource); err != nil {
		return err
	}

	if *jsonOut {
		out := map[string]any{
			"profile":  selectedProfile,
			"space":    spaceName,
			"count":    len(items),
			"messages": items,
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
		return nil
	}

	if len(items) == 0 {
		fmt.Printf("No messages found in %q for profile %q\n", spaceName, selectedProfile)
		return nil
	}
	fmt.Printf("Messages (%d) in %q for profile %q:\n", len(items), spaceName, selectedProfile)
	for _, m := range items {
		when := firstNonEmpty(strings.TrimSpace(m.CreateTime), "unknown-time")
		sender := firstNonEmpty(strings.TrimSpace(m.Sender.DisplayName), strings.TrimSpace(m.Sender.Name), "unknown-sender")
		text := compactMessageText(m.Text)
		fmt.Printf("- %s  %s: %s\n", when, sender, text)
	}
	return nil
}

func runChatMessagesSend(args []string) error {
	fs := flag.NewFlagSet("chat messages send", flag.ContinueOnError)
	profile := fs.String("profile", "", "profile name")
	space := fs.String("space", "", "space resource name or ID")
	email := fs.String("email", "", "recipient email (maps to users/<email>)")
	user := fs.String("user", "", "recipient user resource (users/...)")
	text := fs.String("text", "", "message text to send")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	spaceProvided := strings.TrimSpace(*space) != ""
	recipientProvided := strings.TrimSpace(*email) != "" || strings.TrimSpace(*user) != ""
	if !spaceProvided && !recipientProvided {
		return errors.New("destination required: provide --space or --email/--user")
	}
	if spaceProvided && recipientProvided {
		return errors.New("use either --space or --email/--user, not both")
	}
	if strings.TrimSpace(*email) != "" && strings.TrimSpace(*user) != "" {
		return errors.New("use either --email or --user, not both")
	}
	msgText := strings.TrimSpace(*text)
	if msgText == "" {
		return errors.New("--text is required")
	}

	ctx := context.Background()
	selectedProfile, cfg, st, err := loadAuthContext(*profile)
	if err != nil {
		return err
	}
	oauthCfg := oauthConfigFrom(cfg, st.Scopes)
	tokenSource := oauthCfg.TokenSource(ctx, &st.Token)
	client := oauth2.NewClient(ctx, tokenSource)

	spaceName := ""
	if spaceProvided {
		spaceName = normalizeSpaceName(*space)
	} else {
		targetUser := normalizeUserRef(firstNonEmpty(*user, *email))
		dm, derr := findDirectMessageSpace(ctx, client, targetUser)
		if derr != nil {
			return derr
		}
		spaceName = dm.Name
	}

	sent, err := sendChatMessage(ctx, client, spaceName, msgText)
	if err != nil {
		return err
	}
	if err := saveRefreshedTokenIfChanged(selectedProfile, st, tokenSource); err != nil {
		return err
	}

	if *jsonOut {
		out := map[string]any{
			"profile": selectedProfile,
			"space":   spaceName,
			"message": sent,
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("Sent message to %s\n", spaceName)
	if strings.TrimSpace(sent.Name) != "" {
		fmt.Printf("Message ID: %s\n", sent.Name)
	}
	return nil
}

func runChatMessagesWith(args []string) error {
	fs := flag.NewFlagSet("chat messages with", flag.ContinueOnError)
	profile := fs.String("profile", "", "profile name")
	email := fs.String("email", "", "user email (maps to users/<email>)")
	user := fs.String("user", "", "user resource name (users/...)")
	limit := fs.Int("limit", 10, "max messages to return")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit <= 0 {
		return errors.New("--limit must be greater than 0")
	}
	if strings.TrimSpace(*email) == "" && strings.TrimSpace(*user) == "" {
		return errors.New("one of --email or --user is required")
	}
	if strings.TrimSpace(*email) != "" && strings.TrimSpace(*user) != "" {
		return errors.New("use either --email or --user, not both")
	}

	targetUser := normalizeUserRef(firstNonEmpty(*user, *email))
	ctx := context.Background()
	selectedProfile, cfg, st, err := loadAuthContext(*profile)
	if err != nil {
		return err
	}
	oauthCfg := oauthConfigFrom(cfg, st.Scopes)
	tokenSource := oauthCfg.TokenSource(ctx, &st.Token)
	client := oauth2.NewClient(ctx, tokenSource)

	space, err := findDirectMessageSpace(ctx, client, targetUser)
	if err != nil {
		return err
	}
	items, err := listMessages(ctx, client, space.Name, *limit)
	if err != nil {
		return err
	}
	aliases, _ := loadAliases()
	senderNames, _ := listSpaceSenderNames(ctx, client, space.Name)
	for i := range items {
		if strings.TrimSpace(items[i].Sender.DisplayName) == "" {
			if v := strings.TrimSpace(senderNames[items[i].Sender.Name]); v != "" {
				items[i].Sender.DisplayName = v
				continue
			}
			if v := strings.TrimSpace(aliases[normalizeUserRef(items[i].Sender.Name)]); v != "" {
				items[i].Sender.DisplayName = v
			}
		}
	}
	if err := saveRefreshedTokenIfChanged(selectedProfile, st, tokenSource); err != nil {
		return err
	}

	if *jsonOut {
		out := map[string]any{
			"profile":  selectedProfile,
			"target":   targetUser,
			"space":    space.Name,
			"count":    len(items),
			"messages": items,
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	if len(items) == 0 {
		fmt.Printf("No messages found with %s (%s)\n", targetUser, space.Name)
		return nil
	}
	fmt.Printf("Messages (%d) with %s in %s:\n", len(items), targetUser, space.Name)
	for _, m := range items {
		when := firstNonEmpty(strings.TrimSpace(m.CreateTime), "unknown-time")
		sender := firstNonEmpty(strings.TrimSpace(m.Sender.DisplayName), strings.TrimSpace(m.Sender.Name), "unknown-sender")
		text := compactMessageText(m.Text)
		fmt.Printf("- %s  %s: %s\n", when, sender, text)
	}
	return nil
}

func runChatMessagesSenders(args []string) error {
	fs := flag.NewFlagSet("chat messages senders", flag.ContinueOnError)
	profile := fs.String("profile", "", "profile name")
	space := fs.String("space", "", "space resource name or ID")
	limit := fs.Int("limit", 5, "max sender names to return")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit <= 0 {
		return errors.New("--limit must be greater than 0")
	}
	if strings.TrimSpace(*space) == "" {
		return errors.New("--space is required (example: --space spaces/AAA...)")
	}
	spaceName := normalizeSpaceName(*space)

	ctx := context.Background()
	selectedProfile, cfg, st, err := loadAuthContext(*profile)
	if err != nil {
		return err
	}
	oauthCfg := oauthConfigFrom(cfg, st.Scopes)
	tokenSource := oauthCfg.TokenSource(ctx, &st.Token)
	client := oauth2.NewClient(ctx, tokenSource)

	messageFetchLimit := *limit * 20
	if messageFetchLimit < 50 {
		messageFetchLimit = 50
	}
	if messageFetchLimit > 500 {
		messageFetchLimit = 500
	}

	items, err := listMessages(ctx, client, spaceName, messageFetchLimit)
	if err != nil {
		return err
	}
	aliases, _ := loadAliases()
	senderNames, err := listSpaceSenderNames(ctx, client, spaceName)
	if err != nil {
		return err
	}
	for i := range items {
		if strings.TrimSpace(items[i].Sender.DisplayName) == "" {
			if v := strings.TrimSpace(senderNames[items[i].Sender.Name]); v != "" {
				items[i].Sender.DisplayName = v
				continue
			}
			if v := strings.TrimSpace(aliases[normalizeUserRef(items[i].Sender.Name)]); v != "" {
				items[i].Sender.DisplayName = v
			}
		}
	}
	names := recentSenderNames(items, *limit)
	if err := saveRefreshedTokenIfChanged(selectedProfile, st, tokenSource); err != nil {
		return err
	}

	if *jsonOut {
		out := map[string]any{
			"profile": selectedProfile,
			"space":   spaceName,
			"count":   len(names),
			"names":   names,
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	if len(names) == 0 {
		fmt.Printf("No sender names found in %q\n", spaceName)
		return nil
	}
	fmt.Printf("Recent sender names (%d) in %q:\n", len(names), spaceName)
	for _, n := range names {
		fmt.Printf("- %s\n", n)
	}
	return nil
}

func runChatMessagesPoll(args []string) error {
	fs := flag.NewFlagSet("chat messages poll", flag.ContinueOnError)
	profile := fs.String("profile", "", "profile name")
	space := fs.String("space", "", "optional single space resource name or ID")
	since := fs.Duration("since", 5*time.Minute, "look back window for first poll")
	interval := fs.Duration("interval", 30*time.Second, "poll interval between iterations")
	iterations := fs.Int("iterations", 1, "number of poll iterations")
	limit := fs.Int("limit", 100, "max messages fetched per space per iteration")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *since <= 0 {
		return errors.New("--since must be greater than 0")
	}
	if *iterations <= 0 {
		return errors.New("--iterations must be greater than 0")
	}
	if *interval <= 0 {
		return errors.New("--interval must be greater than 0")
	}
	if *limit <= 0 {
		return errors.New("--limit must be greater than 0")
	}

	ctx := context.Background()
	selectedProfile, cfg, st, err := loadAuthContext(*profile)
	if err != nil {
		return err
	}
	oauthCfg := oauthConfigFrom(cfg, st.Scopes)
	tokenSource := oauthCfg.TokenSource(ctx, &st.Token)
	client := oauth2.NewClient(ctx, tokenSource)
	aliases, _ := loadAliases()

	targetSpaces := []string{}
	if strings.TrimSpace(*space) != "" {
		targetSpaces = append(targetSpaces, normalizeSpaceName(*space))
	} else {
		spaces, lerr := listSpaces(ctx, client, 200)
		if lerr != nil {
			return lerr
		}
		for _, s := range spaces {
			targetSpaces = append(targetSpaces, s.Name)
		}
	}

	cutoff := time.Now().UTC().Add(-*since)
	seen := map[string]struct{}{}
	for i := 0; i < *iterations; i++ {
		iterStart := time.Now().UTC()
		found := make([]PolledMessage, 0, 16)

		for _, sp := range targetSpaces {
			msgs, lerr := listMessages(ctx, client, sp, *limit)
			if lerr != nil {
				continue
			}
			spaceNames, _ := listSpaceSenderNames(ctx, client, sp)
			for _, m := range msgs {
				msgTime, ok := parseMessageTime(m.CreateTime)
				if !ok || msgTime.Before(cutoff) {
					continue
				}
				if _, exists := seen[m.Name]; exists {
					continue
				}
				seen[m.Name] = struct{}{}
				sender := firstNonEmpty(
					strings.TrimSpace(m.Sender.DisplayName),
					strings.TrimSpace(spaceNames[m.Sender.Name]),
					strings.TrimSpace(aliases[normalizeUserRef(m.Sender.Name)]),
					strings.TrimSpace(m.Sender.Name),
				)
				found = append(found, PolledMessage{
					Space:      sp,
					Name:       m.Name,
					CreateTime: m.CreateTime,
					Sender:     sender,
					SenderUser: m.Sender.Name,
					Text:       compactMessageText(m.Text),
				})
			}
		}

		sort.Slice(found, func(a, b int) bool {
			ta, oka := parseMessageTime(found[a].CreateTime)
			tb, okb := parseMessageTime(found[b].CreateTime)
			if !oka || !okb {
				return found[a].CreateTime < found[b].CreateTime
			}
			return ta.Before(tb)
		})

		if *jsonOut {
			out := map[string]any{
				"profile":      selectedProfile,
				"iteration":    i + 1,
				"iterations":   *iterations,
				"since_window": since.String(),
				"count":        len(found),
				"messages":     found,
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			fmt.Println(string(b))
		} else {
			if len(found) == 0 {
				fmt.Printf("[poll %d/%d] no new messages\n", i+1, *iterations)
			} else {
				fmt.Printf("[poll %d/%d] new messages: %d\n", i+1, *iterations, len(found))
				for _, m := range found {
					fmt.Printf("- %s  %s  %s: %s\n", m.CreateTime, m.Space, m.Sender, m.Text)
				}
			}
		}

		cutoff = iterStart
		if i < *iterations-1 {
			time.Sleep(*interval)
		}
	}

	if err := saveRefreshedTokenIfChanged(selectedProfile, st, tokenSource); err != nil {
		return err
	}
	return nil
}

func runAuthSetup(args []string) error {
	fs := flag.NewFlagSet("auth setup", flag.ContinueOnError)
	openLinks := fs.Bool("open", false, "open setup links in browser")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fmt.Println("Google OAuth setup for gchatctl:")
	fmt.Println("1) Enable Google Chat API:")
	fmt.Printf("   %s\n", gcpChatAPIURL)
	fmt.Println("2) Configure OAuth consent screen (External or Internal):")
	fmt.Printf("   %s\n", gcpConsentURL)
	fmt.Println("3) Create OAuth Client ID:")
	fmt.Println("   - Application type: Desktop app (recommended for CLI)")
	fmt.Printf("   - Page: %s\n", gcpCredsURL)
	fmt.Println("4) Copy the Client ID and run:")
	fmt.Println("   gchatctl auth login --client-id <YOUR_CLIENT_ID>")
	fmt.Println()
	fmt.Println("Optional scopes override:")
	fmt.Println("   gchatctl auth login --client-id <YOUR_CLIENT_ID> --scopes https://www.googleapis.com/auth/chat.messages,https://www.googleapis.com/auth/chat.spaces.readonly")

	if !*openLinks {
		return nil
	}
	links := []string{gcpChatAPIURL, gcpConsentURL, gcpCredsURL}
	for _, link := range links {
		if err := openBrowser(link); err != nil {
			fmt.Printf("warning: could not open %s: %v\n", link, err)
		}
	}
	return nil
}

func loadAuthContext(profileFlag string) (string, AppConfig, StoredToken, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", AppConfig{}, StoredToken{}, err
	}
	selectedProfile := chooseProfile(profileFlag, cfg.DefaultProfile)
	st, err := loadToken(selectedProfile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", AppConfig{}, StoredToken{}, fmt.Errorf("profile %q is not authenticated; run: gchatctl auth login --profile %s", selectedProfile, selectedProfile)
		}
		return "", AppConfig{}, StoredToken{}, err
	}
	if strings.TrimSpace(cfg.OAuthClient.ClientID) == "" {
		return "", AppConfig{}, StoredToken{}, errors.New("missing OAuth client ID in config; run `gchatctl auth login` again")
	}
	return selectedProfile, cfg, st, nil
}

func oauthConfigFrom(cfg AppConfig, scopes []string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.OAuthClient.ClientID,
		ClientSecret: cfg.OAuthClient.ClientSecret,
		Scopes:       scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  googleAuthURL,
			TokenURL: googleTokenURL,
		},
	}
}

func saveRefreshedTokenIfChanged(profile string, previous StoredToken, source oauth2.TokenSource) error {
	current, err := source.Token()
	if err != nil {
		return err
	}
	if previous.Token.AccessToken == current.AccessToken &&
		previous.Token.RefreshToken == current.RefreshToken &&
		previous.Token.TokenType == current.TokenType &&
		previous.Token.Expiry.Equal(current.Expiry) {
		return nil
	}
	previous.Token = *current
	previous.SavedAt = time.Now().UTC()
	return saveToken(profile, previous)
}

func normalizeSpaceName(raw string) string {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "spaces/") {
		return s
	}
	return "spaces/" + s
}

func normalizeUserRef(raw string) string {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "users/") {
		return s
	}
	return "users/" + s
}

func compactMessageText(text string) string {
	t := strings.TrimSpace(text)
	if t == "" {
		return "(non-text message)"
	}
	t = strings.ReplaceAll(t, "\r\n", " ")
	t = strings.ReplaceAll(t, "\n", " ")
	t = strings.ReplaceAll(t, "\t", " ")
	if len(t) > 220 {
		return t[:217] + "..."
	}
	return t
}

func listSpaces(ctx context.Context, client *http.Client, limit int) ([]ChatSpace, error) {
	items := make([]ChatSpace, 0, minInt(limit, 100))
	pageToken := ""

	for len(items) < limit {
		pageSize := minInt(limit-len(items), 100)
		u, err := url.Parse("https://chat.googleapis.com/v1/spaces")
		if err != nil {
			return nil, err
		}
		q := u.Query()
		q.Set("pageSize", fmt.Sprintf("%d", pageSize))
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		u.RawQuery = q.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		var parsed ListSpacesResponse
		if err := decodeAPIResponse(resp, &parsed); err != nil {
			return nil, err
		}
		items = append(items, parsed.Spaces...)
		if parsed.NextPageToken == "" || len(parsed.Spaces) == 0 {
			break
		}
		pageToken = parsed.NextPageToken
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func listMessages(ctx context.Context, client *http.Client, spaceName string, limit int) ([]ChatMessage, error) {
	items := make([]ChatMessage, 0, minInt(limit, 100))
	pageToken := ""

	for len(items) < limit {
		pageSize := minInt(limit-len(items), 100)
		base := fmt.Sprintf("https://chat.googleapis.com/v1/%s/messages", spaceName)
		u, err := url.Parse(base)
		if err != nil {
			return nil, err
		}
		q := u.Query()
		q.Set("pageSize", fmt.Sprintf("%d", pageSize))
		q.Set("orderBy", "createTime desc")
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		u.RawQuery = q.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		var parsed ListMessagesResponse
		if err := decodeAPIResponse(resp, &parsed); err != nil {
			return nil, err
		}
		items = append(items, parsed.Messages...)
		if parsed.NextPageToken == "" || len(parsed.Messages) == 0 {
			break
		}
		pageToken = parsed.NextPageToken
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func sendChatMessage(ctx context.Context, client *http.Client, spaceName, text string) (ChatMessage, error) {
	var out ChatMessage
	body := map[string]string{"text": text}
	b, err := json.Marshal(body)
	if err != nil {
		return out, err
	}
	u := fmt.Sprintf("https://chat.googleapis.com/v1/%s/messages", normalizeSpaceName(spaceName))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(string(b)))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return out, err
	}
	if err := decodeAPIResponse(resp, &out); err != nil {
		return out, err
	}
	return out, nil
}

func findDirectMessageSpace(ctx context.Context, client *http.Client, userName string) (ChatSpace, error) {
	var out ChatSpace
	u, err := url.Parse("https://chat.googleapis.com/v1/spaces:findDirectMessage")
	if err != nil {
		return out, err
	}
	q := u.Query()
	q.Set("name", userName)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return out, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return out, err
	}
	if err := decodeAPIResponse(resp, &out); err != nil {
		return out, err
	}
	if strings.TrimSpace(out.Name) == "" {
		return out, fmt.Errorf("no direct message found for %s", userName)
	}
	return out, nil
}

func getSpaceReadState(ctx context.Context, client *http.Client, spaceName string) (SpaceReadState, error) {
	var out SpaceReadState
	spaceID := strings.TrimPrefix(normalizeSpaceName(spaceName), "spaces/")
	u := fmt.Sprintf("https://chat.googleapis.com/v1/users/me/spaces/%s/spaceReadState", url.PathEscape(spaceID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return out, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return out, err
	}
	if err := decodeAPIResponse(resp, &out); err != nil {
		return out, err
	}
	return out, nil
}

func parseMessageTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err == nil {
		return t.UTC(), true
	}
	t, err = time.Parse(time.RFC3339, raw)
	if err == nil {
		return t.UTC(), true
	}
	return time.Time{}, false
}

func listSpaceSenderNames(ctx context.Context, client *http.Client, spaceName string) (map[string]string, error) {
	out := map[string]string{}
	members, err := listSpaceMembers(ctx, client, spaceName)
	if err != nil {
		return nil, err
	}
	for _, m := range members {
		id := strings.TrimSpace(m.Member.Name)
		name := strings.TrimSpace(m.Member.DisplayName)
		if id == "" || name == "" {
			continue
		}
		out[id] = name
	}
	return out, nil
}

func listSpaceMembers(ctx context.Context, client *http.Client, spaceName string) ([]ChatMembership, error) {
	out := make([]ChatMembership, 0, 16)
	pageToken := ""

	for {
		u, err := url.Parse(fmt.Sprintf("https://chat.googleapis.com/v1/%s/members", spaceName))
		if err != nil {
			return nil, err
		}
		q := u.Query()
		q.Set("pageSize", "200")
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		u.RawQuery = q.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		var parsed ListMembershipsResponse
		if err := decodeAPIResponse(resp, &parsed); err != nil {
			return nil, err
		}
		out = append(out, parsed.Memberships...)
		if parsed.NextPageToken == "" {
			break
		}
		pageToken = parsed.NextPageToken
	}
	return out, nil
}

func currentUserRef(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://chat.googleapis.com/v1/users/me", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	var u ChatUserResource
	if err := decodeAPIResponse(resp, &u); err != nil {
		return "", err
	}
	return strings.TrimSpace(u.Name), nil
}

func dmPeerForSpace(ctx context.Context, client *http.Client, spaceName, currentUser string) (string, string, error) {
	members, err := listSpaceMembers(ctx, client, spaceName)
	if err != nil {
		return "", "", err
	}
	cur := strings.TrimSpace(currentUser)
	var fallbackUser string
	var fallbackName string
	for _, m := range members {
		if strings.ToUpper(strings.TrimSpace(m.Member.Type)) != "HUMAN" {
			continue
		}
		id := strings.TrimSpace(m.Member.Name)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(m.Member.DisplayName)
		if fallbackUser == "" {
			fallbackUser = id
			fallbackName = name
		}
		if cur != "" && id == cur {
			continue
		}
		return id, name, nil
	}
	return fallbackUser, fallbackName, nil
}

func inferCurrentUserFromDMS(ctx context.Context, client *http.Client, spaces []ChatSpace) string {
	counts := map[string]int{}
	for _, s := range spaces {
		if s.SpaceType != "DIRECT_MESSAGE" {
			continue
		}
		members, err := listSpaceMembers(ctx, client, s.Name)
		if err != nil {
			continue
		}
		for _, m := range members {
			if strings.ToUpper(strings.TrimSpace(m.Member.Type)) != "HUMAN" {
				continue
			}
			id := normalizeUserRef(m.Member.Name)
			if id == "" || id == "users/" {
				continue
			}
			counts[id]++
		}
	}
	bestID := ""
	bestCount := 0
	for id, n := range counts {
		if n > bestCount {
			bestID = id
			bestCount = n
		}
	}
	return bestID
}

func filterMessagesByPerson(messages []ChatMessage, person string) []ChatMessage {
	q := strings.ToLower(strings.TrimSpace(person))
	if q == "" {
		return messages
	}
	out := make([]ChatMessage, 0, len(messages))
	for _, m := range messages {
		name := strings.ToLower(strings.TrimSpace(m.Sender.DisplayName))
		id := strings.ToLower(strings.TrimSpace(m.Sender.Name))
		shortID := strings.TrimPrefix(id, "users/")
		if strings.Contains(name, q) || strings.Contains(id, q) || shortID == q {
			out = append(out, m)
		}
	}
	return out
}

func recentSenderNames(messages []ChatMessage, limit int) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, minInt(limit, len(messages)))
	for _, m := range messages {
		n := firstNonEmpty(strings.TrimSpace(m.Sender.DisplayName), strings.TrimSpace(m.Sender.Name))
		if n == "" {
			continue
		}
		key := strings.ToLower(n)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, n)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func decodeAPIResponse(resp *http.Response, out any) error {
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		trimmed := strings.TrimSpace(string(body))
		if trimmed == "" {
			trimmed = resp.Status
		}
		var apiErr GoogleAPIErrorEnvelope
		if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error.Message != "" {
			msg := strings.TrimSpace(apiErr.Error.Message)
			if strings.Contains(strings.ToLower(msg), "google chat app not found") {
				return errors.New("google chat app not found in this project; enable Chat API and configure a Chat app in Google Cloud Console (gchatctl auth setup shows links)")
			}
			if apiErr.Error.Code == 403 && strings.Contains(strings.ToLower(msg), "insufficient authentication scopes") {
				return errors.New("insufficient auth scopes; run `gchatctl auth login --profile <profile> --all-scopes`")
			}
			return fmt.Errorf("google chat api request failed (%s): %s", resp.Status, msg)
		}
		return fmt.Errorf("google chat api request failed (%s): %s", resp.Status, trimmed)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return err
	}
	return nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func aliasesPath() (string, error) {
	d, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "aliases.json"), nil
}

func loadAliases() (map[string]string, error) {
	p, err := aliasesPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	var cfg AliasConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	if cfg.Aliases == nil {
		cfg.Aliases = map[string]string{}
	}
	return cfg.Aliases, nil
}

func saveAliases(aliases map[string]string) error {
	p, err := aliasesPath()
	if err != nil {
		return err
	}
	cfg := AliasConfig{
		Aliases:   aliases,
		UpdatedAt: time.Now().UTC(),
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

func runAuthLogin(args []string) error {
	fs := flag.NewFlagSet("auth login", flag.ContinueOnError)
	profile := fs.String("profile", "", "profile name")
	clientID := fs.String("client-id", "", "OAuth client ID")
	clientSecret := fs.String("client-secret", "", "OAuth client secret")
	scopesRaw := fs.String("scopes", "", "comma-separated OAuth scopes")
	allScopes := fs.Bool("all-scopes", false, "use recommended full chat read scopes")
	mode := fs.String("mode", "auto", "auth mode: auto, browser, device")
	noOpen := fs.Bool("no-open", false, "do not open browser automatically")
	timeout := fs.Duration("timeout", 3*time.Minute, "browser callback timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	selectedProfile := chooseProfile(*profile, cfg.DefaultProfile)
	effectiveScopesRaw := *scopesRaw
	if *allScopes {
		effectiveScopesRaw = defaultChatScopesCSV
	}
	scopes := chooseScopes(effectiveScopesRaw, cfg.Scopes)
	if len(scopes) == 0 {
		scopes = append([]string(nil), defaultChatScopes...)
	}

	cid := firstNonEmpty(*clientID, os.Getenv("GCHATCTL_CLIENT_ID"), cfg.OAuthClient.ClientID)
	secret := firstNonEmpty(*clientSecret, os.Getenv("GCHATCTL_CLIENT_SECRET"), cfg.OAuthClient.ClientSecret)
	if cid == "" {
		if !isInteractive() {
			return errors.New("missing client ID; pass --client-id or set GCHATCTL_CLIENT_ID (create one in Google Cloud Console: APIs & Services > Credentials)")
		}
		printOAuthClientIDHelp()
		v, perr := prompt("Google OAuth client ID: ")
		if perr != nil {
			return perr
		}
		cid = strings.TrimSpace(v)
	}

	if *timeout <= 0 {
		return errors.New("--timeout must be greater than 0")
	}

	resolvedMode := resolveMode(*mode, *noOpen, isInteractive())
	if resolvedMode == "" {
		return errors.New("invalid --mode, expected auto|browser|device")
	}

	ctx := context.Background()
	var tok *oauth2.Token
	switch resolvedMode {
	case "browser":
		tok, err = loginBrowserFlow(ctx, cid, secret, scopes, *noOpen, *timeout)
	case "device":
		tok, err = loginDeviceFlow(ctx, cid, secret, scopes)
	default:
		return fmt.Errorf("unsupported mode %q", resolvedMode)
	}
	if err != nil {
		return err
	}

	cfg.DefaultProfile = selectedProfile
	cfg.OAuthClient.ClientID = cid
	cfg.OAuthClient.ClientSecret = secret
	cfg.Scopes = scopes
	if err := saveConfig(cfg); err != nil {
		return err
	}
	if err := saveToken(selectedProfile, StoredToken{Token: *tok, Scopes: scopes, Mode: resolvedMode, SavedAt: time.Now().UTC()}); err != nil {
		return err
	}

	fmt.Printf("Logged in profile %q using %s flow.\n", selectedProfile, resolvedMode)
	if strings.TrimSpace(secret) == "" {
		fmt.Println("Client secret: not set (PKCE/public client mode)")
	}
	if tok.Expiry.IsZero() {
		fmt.Println("Token expiry: none")
	} else {
		fmt.Println("Token expiry:", tok.Expiry.Format(time.RFC3339))
	}
	return nil
}

func runAuthStatus(args []string) error {
	fs := flag.NewFlagSet("auth status", flag.ContinueOnError)
	profile := fs.String("profile", "", "profile name")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	selectedProfile := chooseProfile(*profile, cfg.DefaultProfile)
	if selectedProfile == "" {
		selectedProfile = "default"
	}

	st, err := loadToken(selectedProfile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if *jsonOut {
				fmt.Printf("{\"profile\":%q,\"authenticated\":false}\n", selectedProfile)
				return nil
			}
			fmt.Printf("Profile %q: not authenticated\n", selectedProfile)
			return nil
		}
		return err
	}

	valid := st.Token.Valid()
	refreshTokenPresent := strings.TrimSpace(st.Token.RefreshToken) != ""
	hasTokenMaterial := strings.TrimSpace(st.Token.AccessToken) != "" || refreshTokenPresent
	tokenFile, _ := tokenPath(selectedProfile)
	status := map[string]any{
		"profile":               selectedProfile,
		"authenticated":         hasTokenMaterial,
		"valid":                 valid,
		"expiry":                st.Token.Expiry,
		"saved_at":              st.SavedAt,
		"mode":                  st.Mode,
		"scopes":                st.Scopes,
		"refresh_token_present": refreshTokenPresent,
		"token_path":            tokenFile,
	}

	if *jsonOut {
		b, _ := json.MarshalIndent(status, "", "  ")
		fmt.Println(string(b))
		return nil
	}

	fmt.Printf("Profile: %s\n", selectedProfile)
	fmt.Printf("Authenticated: %v\n", hasTokenMaterial)
	fmt.Printf("Valid now: %v\n", valid)
	if st.Token.Expiry.IsZero() {
		fmt.Println("Expiry: none")
	} else {
		fmt.Println("Expiry:", st.Token.Expiry.Format(time.RFC3339))
	}
	fmt.Printf("Refresh token: %v\n", refreshTokenPresent)
	if !st.SavedAt.IsZero() {
		fmt.Println("Saved at:", st.SavedAt.Format(time.RFC3339))
	}
	fmt.Println("Mode:", st.Mode)
	fmt.Println("Scopes:", strings.Join(st.Scopes, ", "))
	if tokenFile != "" {
		fmt.Println("Token file:", tokenFile)
	}
	return nil
}

func runAuthLogout(args []string) error {
	fs := flag.NewFlagSet("auth logout", flag.ContinueOnError)
	profile := fs.String("profile", "", "profile name")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	selectedProfile := chooseProfile(*profile, cfg.DefaultProfile)
	if selectedProfile == "" {
		selectedProfile = "default"
	}

	err = deleteToken(selectedProfile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	fmt.Printf("Removed token for profile %q\n", selectedProfile)
	return nil
}

func loginBrowserFlow(ctx context.Context, clientID, clientSecret string, scopes []string, noOpen bool, timeout time.Duration) (*oauth2.Token, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer ln.Close()

	redirectURI := fmt.Sprintf("http://%s/callback", ln.Addr().String())
	oauthCfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURI,
		Scopes:       scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  googleAuthURL,
			TokenURL: googleTokenURL,
		},
	}

	state, err := randomString(24)
	if err != nil {
		return nil, err
	}
	codeVerifier, err := randomString(64)
	if err != nil {
		return nil, err
	}
	codeChallenge := pkceChallenge(codeVerifier)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := &http.Server{}
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			errCh <- errors.New("state mismatch")
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			errCh <- errors.New("missing auth code")
			return
		}
		_, _ = io.WriteString(w, "gchatctl login complete. You can close this tab.")
		codeCh <- code
	})
	srv.Handler = mux

	go func() {
		_ = srv.Serve(ln)
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	authURL := oauthCfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)

	fmt.Println("Open this URL to authorize:")
	fmt.Println(authURL)
	if !noOpen {
		if oerr := openBrowser(authURL); oerr != nil {
			fmt.Println("warning: could not open browser automatically:", oerr)
		}
	}

	select {
	case code := <-codeCh:
		token, xerr := oauthCfg.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", codeVerifier))
		if xerr != nil {
			return nil, xerr
		}
		if token.RefreshToken == "" {
			fmt.Println("warning: no refresh token returned; try revoking prior consent and log in again")
		}
		return token, nil
	case e := <-errCh:
		return nil, e
	case <-time.After(timeout):
		return nil, fmt.Errorf("timed out waiting for browser callback after %s", timeout)
	}
}

func loginDeviceFlow(ctx context.Context, clientID, clientSecret string, scopes []string) (*oauth2.Token, error) {
	v := url.Values{}
	v.Set("client_id", clientID)
	v.Set("scope", strings.Join(scopes, " "))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleDeviceURL, strings.NewReader(v.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("device code request failed: %s", strings.TrimSpace(string(b)))
	}

	var dc DeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&dc); err != nil {
		return nil, err
	}

	if dc.Interval <= 0 {
		dc.Interval = 5
	}
	fmt.Println("Use this device code to authorize:")
	fmt.Printf("  Code: %s\n", dc.UserCode)
	if dc.VerificationURLComplete != "" {
		fmt.Printf("  URL:  %s\n", dc.VerificationURLComplete)
	} else {
		fmt.Printf("  URL:  %s\n", dc.VerificationURL)
	}
	fmt.Println("Waiting for approval...")

	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
	interval := time.Duration(dc.Interval) * time.Second

	for time.Now().Before(deadline) {
		tok, pending, slowDown, err := pollDeviceToken(ctx, clientID, clientSecret, dc.DeviceCode)
		if err != nil {
			return nil, err
		}
		if tok != nil {
			return tok, nil
		}
		if pending {
			if slowDown {
				interval += 5 * time.Second
			}
			time.Sleep(interval)
			continue
		}
	}

	return nil, errors.New("device login timed out")
}

func pollDeviceToken(ctx context.Context, clientID, clientSecret, deviceCode string) (*oauth2.Token, bool, bool, error) {
	v := url.Values{}
	v.Set("client_id", clientID)
	if strings.TrimSpace(clientSecret) != "" {
		v.Set("client_secret", clientSecret)
	}
	v.Set("device_code", deviceCode)
	v.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL, strings.NewReader(v.Encode()))
	if err != nil {
		return nil, false, false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false, false, err
	}
	defer resp.Body.Close()

	var tr DeviceTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, false, false, err
	}

	if tr.Error != "" {
		switch tr.Error {
		case "authorization_pending":
			return nil, true, false, nil
		case "slow_down":
			return nil, true, true, nil
		case "access_denied":
			return nil, false, false, errors.New("authorization denied")
		case "expired_token":
			return nil, false, false, errors.New("device code expired")
		default:
			return nil, false, false, fmt.Errorf("device token error: %s", tr.Error)
		}
	}

	if tr.AccessToken == "" {
		return nil, true, false, nil
	}

	tok := &oauth2.Token{
		AccessToken:  tr.AccessToken,
		TokenType:    tr.TokenType,
		RefreshToken: tr.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
	}
	return tok, false, false, nil
}

func openBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}

func chooseProfile(flagVal, defaultProfile string) string {
	p := strings.TrimSpace(firstNonEmpty(flagVal, os.Getenv("GCHATCTL_PROFILE"), defaultProfile))
	if p == "" {
		return "default"
	}
	return p
}

func chooseScopes(flagRaw string, defaultScopes []string) []string {
	fromEnv := os.Getenv("GCHATCTL_SCOPES")
	raw := strings.TrimSpace(firstNonEmpty(flagRaw, fromEnv))
	if raw == "" {
		return uniqueScopes(defaultScopes)
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			out = append(out, s)
		}
	}
	return uniqueScopes(out)
}

func uniqueScopes(scopes []string) []string {
	seen := make(map[string]struct{}, len(scopes))
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		s := strings.TrimSpace(scope)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func resolveMode(mode string, noOpen, interactive bool) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "auto":
		if noOpen || !interactive {
			return "device"
		}
		return "browser"
	case "browser", "device":
		return strings.ToLower(mode)
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func configDir() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(root, "gchatctl")
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, nil
}

func configPath() (string, error) {
	d, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.json"), nil
}

func tokenPath(profile string) (string, error) {
	d, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, fmt.Sprintf("token_%s.json", safeName(profile))), nil
}

func safeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "default"
	}
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, ":", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

func loadConfig() (AppConfig, error) {
	var cfg AppConfig
	cfg.DefaultProfile = "default"
	path, err := configPath()
	if err != nil {
		return cfg, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	if cfg.DefaultProfile == "" {
		cfg.DefaultProfile = "default"
	}
	return cfg, nil
}

func saveConfig(cfg AppConfig) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return err
	}
	return nil
}

func loadToken(profile string) (StoredToken, error) {
	var st StoredToken
	p, err := tokenPath(profile)
	if err != nil {
		return st, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, err
	}
	return st, nil
}

func saveToken(profile string, st StoredToken) error {
	p, err := tokenPath(profile)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		return err
	}
	return nil
}

func deleteToken(profile string) error {
	p, err := tokenPath(profile)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

func randomString(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func printOAuthClientIDHelp() {
	fmt.Println("OAuth setup needed once:")
	fmt.Println("  1) Open Google Cloud Console > APIs & Services > Credentials")
	fmt.Println("  2) Create OAuth Client ID (Desktop app)")
	fmt.Println("  3) Paste the Client ID below")
	fmt.Println("Tip: run `gchatctl auth setup` for direct links.")
	fmt.Println("Client secret is optional for browser login.")
}

func prompt(label string) (string, error) {
	fmt.Print(label)
	var value string
	_, err := fmt.Scanln(&value)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return "", errors.New("input aborted")
		}
		return "", err
	}
	return value, nil
}

func isInteractive() bool {
	st, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (st.Mode() & os.ModeCharDevice) != 0
}
