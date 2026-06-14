package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"connectrpc.com/connect"
	libopsv1 "github.com/libops/proto/libops/v1"
	"github.com/libops/sitectl-libops/pkg/api"
	"github.com/spf13/cobra"
)

var createSSHKeyCmd = &cobra.Command{
	Use:     "ssh-key",
	Aliases: []string{"sshkey"},
	Short:   "Add an SSH public key to your LibOps account",
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}

		publicKey, err := sshPublicKeyFromFlags(cmd)
		if err != nil {
			return err
		}
		name, _ := cmd.Flags().GetString("name")
		accountID, _ := cmd.Flags().GetString("account-id")

		client, err := api.NewLibopsAPIClient(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}

		resp, err := client.SshKeyService.CreateSshKey(cmd.Context(), connect.NewRequest(&libopsv1.CreateSshKeyRequest{
			AccountId: accountID,
			PublicKey: publicKey,
			Name:      optionalString(strings.TrimSpace(name)),
		}))
		if err != nil {
			return fmt.Errorf("failed to add SSH key: %w", err)
		}

		key := resp.Msg.GetSshKey()
		fmt.Printf("Added SSH key: %s\n", key.GetKeyId())
		if key.GetFingerprint() != "" {
			fmt.Printf("Fingerprint: %s\n", key.GetFingerprint())
		}
		fmt.Println("GitHub Checkout: ensure this public key is also added to GitHub.")
		return nil
	},
}

var listSSHKeysCmd = &cobra.Command{
	Use:     "ssh-keys",
	Aliases: []string{"sshkeys", "ssh-key"},
	Short:   "List SSH keys on your LibOps account",
	RunE: func(cmd *cobra.Command, args []string) error {
		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		accountID, _ := cmd.Flags().GetString("account-id")

		client, err := api.NewLibopsAPIClient(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}

		resp, err := client.SshKeyService.ListSshKeys(cmd.Context(), connect.NewRequest(&libopsv1.ListSshKeysRequest{
			AccountId: accountID,
		}))
		if err != nil {
			return fmt.Errorf("failed to list SSH keys: %w", err)
		}
		if len(resp.Msg.SshKeys) == 0 {
			fmt.Println("No SSH keys found")
			return nil
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "KEY ID\tNAME\tFINGERPRINT")
		for _, key := range resp.Msg.SshKeys {
			fmt.Fprintf(w, "%s\t%s\t%s\n", key.GetKeyId(), valueOrDash(key.GetName()), valueOrDash(key.GetFingerprint()))
		}
		return w.Flush()
	},
}

var deleteSSHKeyCmd = &cobra.Command{
	Use:     "ssh-key <key-id>",
	Aliases: []string{"sshkey"},
	Short:   "Delete an SSH key from your LibOps account",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		keyID := args[0]
		confirmed, err := confirmDeletion(cmd, "SSH key", keyID)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("Deletion cancelled.")
			return nil
		}

		apiBaseURL, err := cmd.Flags().GetString("api-url")
		if err != nil {
			return err
		}
		accountID, _ := cmd.Flags().GetString("account-id")

		client, err := api.NewLibopsAPIClient(cmd.Context(), apiBaseURL)
		if err != nil {
			return err
		}

		_, err = client.SshKeyService.DeleteSshKey(cmd.Context(), connect.NewRequest(&libopsv1.DeleteSshKeyRequest{
			AccountId: accountID,
			KeyId:     keyID,
		}))
		if err != nil {
			return fmt.Errorf("failed to delete SSH key: %w", err)
		}

		fmt.Printf("Deleted SSH key: %s\n", keyID)
		return nil
	},
}

func sshPublicKeyFromFlags(cmd *cobra.Command) (string, error) {
	publicKey, _ := cmd.Flags().GetString("public-key")
	publicKeyFile, _ := cmd.Flags().GetString("public-key-file")
	if strings.TrimSpace(publicKey) != "" && strings.TrimSpace(publicKeyFile) != "" {
		return "", fmt.Errorf("specify only one of --public-key or --public-key-file")
	}
	if strings.TrimSpace(publicKeyFile) != "" {
		data, err := os.ReadFile(publicKeyFile) // #nosec G304 -- user explicitly selects the public key file.
		if err != nil {
			return "", fmt.Errorf("read public key file: %w", err)
		}
		publicKey = string(data)
	}
	publicKey = strings.TrimSpace(publicKey)
	if publicKey == "" {
		return "", fmt.Errorf("must specify --public-key or --public-key-file")
	}
	return publicKey, nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func valueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func init() {
	createCmd.AddCommand(createSSHKeyCmd)
	listCmd.AddCommand(listSSHKeysCmd)
	deleteCmd.AddCommand(deleteSSHKeyCmd)

	createSSHKeyCmd.Flags().String("account-id", "", "Account ID; defaults to the authenticated account")
	createSSHKeyCmd.Flags().String("name", "", "SSH key name")
	createSSHKeyCmd.Flags().String("public-key", "", "SSH public key content")
	createSSHKeyCmd.Flags().String("public-key-file", "", "Path to SSH public key file")

	listSSHKeysCmd.Flags().String("account-id", "", "Account ID; defaults to the authenticated account")

	deleteSSHKeyCmd.Flags().String("account-id", "", "Account ID; defaults to the authenticated account")
	deleteSSHKeyCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
}
