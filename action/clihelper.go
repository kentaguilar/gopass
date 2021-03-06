package action

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/justwatchcom/gopass/gpg"
	"golang.org/x/crypto/ssh/terminal"
)

// confirmRecipients asks the user to confirm a given set of recipients
func (s *Action) confirmRecipients(name string, recipients []string) ([]string, error) {
	if s.Store.NoConfirm {
		return recipients, nil
	}
	for {
		fmt.Printf("gopass: Encrypting %s for these recipients:\n", name)
		sort.Strings(recipients)
		for _, r := range recipients {
			kl, err := gpg.ListPublicKeys(r)
			if err != nil {
				fmt.Println(err)
				continue
			}
			if len(kl) < 1 {
				fmt.Println("key not found", r)
				continue
			}
			fmt.Printf(" - %s\n", kl[0].OneLine())
		}
		fmt.Println("")

		yes, err := askForBool("Do you want to continue?", true)
		if err != nil {
			return recipients, err
		}

		if yes {
			return recipients, nil
		}

		return recipients, fmt.Errorf("user aborted")
	}
}

// clearClipboard will spwan a copy of gopass that waits in a detached background
// process group until the timeout is expired. It will then compare the contents
// of the clipboard and erase it if it still contains the data gopass copied
// to it.
func clearClipboard(content []byte, timeout int) error {
	hash := fmt.Sprintf("%x", sha256.Sum256(content))

	cmd := exec.Command(os.Args[0], "unclip", "--timeout", strconv.Itoa(timeout))
	// https://groups.google.com/d/msg/golang-nuts/shST-SDqIp4/za4oxEiVtI0J
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	cmd.Env = append(os.Environ(), "GOPASS_UNCLIP_CHECKSUM="+hash)
	return cmd.Start()
}

// askForConfirmation asks a yes/no question until the user
// replies yes or no
func askForConfirmation(text string) bool {
	for {
		if choice, err := askForBool(text, false); err == nil {
			return choice
		}
	}
}

// askForBool ask for a bool (yes or no) exactly once.
// The empty answer uses the specified default, any other answer
// is an error.
func askForBool(text string, def bool) (bool, error) {
	choices := "y/N"
	if def {
		choices = "Y/n"
	}

	str, err := askForString(text, choices)
	if err != nil {
		return false, err
	}
	switch str {
	case "Y/n":
		return true, nil
	case "y/N":
		return false, nil
	}

	str = strings.ToLower(string(str[0]))
	switch str {
	case "y":
		return true, nil
	case "n":
		return false, nil
	default:
		return false, fmt.Errorf("Unknown answer: %s", str)
	}
}

// askForString asks for a string once, using the default if the
// anser is empty. Errors are only returned on I/O errors
func askForString(text, def string) (string, error) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Printf("%s [%s]: ", text, def)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	input = strings.TrimSpace(input)
	if input == "" {
		input = def
	}
	return input, nil
}

// askForInt asks for an valid interger once. If the input
// can not be converted to an int it returns an error
func askForInt(text string, def int) (int, error) {
	str, err := askForString(text, strconv.Itoa(def))
	if err != nil {
		return 0, err
	}
	intVal, err := strconv.Atoi(str)
	if err != nil {
		return 0, err
	}
	return intVal, nil
}

// askForPassword prompts for a password twice until both match
func askForPassword(name string, askFn func(string) (string, error)) (string, error) {
	if askFn == nil {
		askFn = promptPass
	}
	for {
		pass, err := askFn(fmt.Sprintf("Enter password for %s", name))
		if err != nil {
			return "", err
		}

		passAgain, err := askFn(fmt.Sprintf("Retype password for %s", name))
		if err != nil {
			return "", err
		}

		if pass == passAgain {
			return strings.TrimSpace(pass), nil
		}

		fmt.Println("Error: the entered password do not match")
	}
}

// askForKeyImport asks for permissions to import the named key
func askForKeyImport(key string) bool {
	ok, err := askForBool("Do you want to import the public key '%s' into your keyring?", false)
	if err != nil {
		return false
	}
	return ok
}

// askforPrivateKey promts the user to select from a list of private keys
func askForPrivateKey(prompt string) (string, error) {
	kl, err := gpg.ListPrivateKeys()
	if err != nil {
		return "", err
	}
	kl = kl.UseableKeys()
	if len(kl) < 1 {
		return "", fmt.Errorf("No useable private keys found")
	}
	for {
		fmt.Println(prompt)
		for i, k := range kl {
			fmt.Printf("[%d] %s\n", i, k.OneLine())
		}
		iv, err := askForInt(fmt.Sprintf("Please enter the number of a key (0-%d)", len(kl)-1), 0)
		if err != nil {
			continue
		}
		if iv >= 0 && iv < len(kl) {
			return kl[iv].Fingerprint, nil
		}
	}
}

// promptPass will prompt user's for a password by terminal.
func promptPass(prompt string) (pass string, err error) {
	// Make a copy of STDIN's state to restore afterward
	fd := int(os.Stdin.Fd())
	oldState, err := terminal.GetState(fd)
	if err != nil {
		return "", fmt.Errorf("Could not get state of terminal: %s", err)
	}
	defer func() {
		if err := terminal.Restore(fd, oldState); err != nil {
			fmt.Printf("Failed to restore terminal: %s\n", err)
		}
	}()

	// Restore STDIN in the event of a signal interruption
	sigch := make(chan os.Signal, 1)
	signal.Notify(sigch, os.Interrupt)
	go func() {
		for range sigch {
			if err := terminal.Restore(fd, oldState); err != nil {
				fmt.Printf("Failed to restore terminal: %s\n", err)
			}
			os.Exit(1)
		}
	}()

	fmt.Printf("%s: ", prompt)
	passBytes, err := terminal.ReadPassword(fd)
	fmt.Println("")
	return string(passBytes), err
}
