package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/howeyc/gopass"
	"golang.org/x/crypto/nacl/secretbox"
	"golang.org/x/crypto/scrypt"
)

/* should switch to the codahale/chacha20poly1305 package.  Or whatever we're
using for LNDC; otherwise it's 2 almost-the-same stream ciphers to import */

// warning! look at those imports! crypto! hopefully this works!

/* on-disk stored keys are 32bytes.  This is good for ed25519 private keys,
for seeds for bip32, for individual secp256k1 priv keys, and so on.
32 bytes is enough for anyone.
If you want fewer bytes, put some zeroes at the end */

// LoadKeyFromFileInteractive opens the file 'filename' and presents a
// keyboard prompt for the passphrase to decrypt it.  It returns the
// key if decryption works, or errors out.
func LoadKeyFromFileInteractive(filename string) (*[96]byte, error) {
	a, err := os.Stat(filename)
	if err != nil {
		return nil, err
	}
	if a.Size() < 193 { // there can't be a password...
		return LoadKeyFromFileArg(filename, nil)
	}
	fmt.Printf("passphrase: ")
	pass, err := gopass.GetPasswd()
	if err != nil {
		return nil, err
	}
	fmt.Printf("\n")
	return LoadKeyFromFileArg(filename, pass)
}

// LoadKeyFromFileArg opens the file and returns the key.  If the key is
// unencrypted it will ignore the password argument.
func LoadKeyFromFileArg(filename string, pass []byte) (*[96]byte, error) {
	priv96 := new([96]byte)
	keyhex, err := ioutil.ReadFile(filename)
	if err != nil {
		return priv96, err
	}
	keyhex = []byte(strings.TrimSpace(string(keyhex)))
	enckey, err := hex.DecodeString(string(keyhex))
	if err != nil {
		return priv96, err
	}

	if len(enckey) == 96 { // UNencrypted key, length 32
		fmt.Printf("WARNING!! Key file not encrypted!!\n")
		fmt.Printf("Anyone who can read the key file can take everything!\n")
		fmt.Printf("You should start over and use a good passphrase!\n")
		copy(priv96[:], enckey[:])
		return priv96, nil
	}
	// enckey should be 136 bytes.  24 for scrypt salt/box nonce,
	// 16 for box auth
	if len(enckey) != 136 {
		return priv96, fmt.Errorf("Key length error for %s ", filename)
	}
	// enckey is actually encrypted, get derived key from pass and salt
	// first extract salt
	salt := new([24]byte)      // salt (also nonce for secretbox)
	dk32 := new([32]byte)      // derived key array
	copy(salt[:], enckey[:24]) // first 24 bytes are scrypt salt/box nonce

	dk, err := scrypt.Key(pass, salt[:], 16384, 8, 1, 32) // derive key
	if err != nil {
		return priv96, err
	}
	copy(dk32[:], dk[:]) // copy into fixed size array

	// nonce for secretbox is the same as scrypt salt.  Seems fine.  Really.
	priv, worked := secretbox.Open(nil, enckey[24:], salt, dk32)
	if worked != true {
		return priv96, fmt.Errorf("Decryption failed for %s ", filename)
	}
	copy(priv96[:], priv[:]) //copy decrypted private key into array

	priv = nil // this probably doesn't do anything but... eh why not
	return priv96, nil
}

// saves a 32 byte key to file, prompting for passphrase.
// if user enters empty passphrase (hits enter twice), will be saved
// in the clear.
func SaveKeyToFileInteractive(filename string, priv *[96]byte) error {
	var match bool
	var err error
	var pass1, pass2 []byte
	for match != true {
		fmt.Printf("passphrase: ")
		pass1, err = gopass.GetPasswd()
		if err != nil {
			return err
		}
		fmt.Printf("repeat passphrase: ")
		pass2, err = gopass.GetPasswd()
		if err != nil {
			return err
		}
		if string(pass1) == string(pass2) {
			match = true
		} else {
			fmt.Printf("user input error.  Try again gl hf dd.\n")
		}
	}
	fmt.Printf("\n")
	return SaveKeyToFileArg(filename, priv, pass1)
}

// saves a 96 byte key (three points) to a file, encrypting with pass.
// if pass is nil or zero length, doesn't encrypt and just saves in hex.
func SaveKeyToFileArg(filename string, priv *[96]byte, pass []byte) error {
	if len(pass) == 0 { // zero-length pass, save unencrypted
		keyhex := fmt.Sprintf("%x\n", priv[:])
		err := ioutil.WriteFile(filename, []byte(keyhex), 0600)
		if err != nil {
			return err
		}
		fmt.Printf("WARNING!! Key file not encrypted!!\n")
		fmt.Printf("Anyone who can read the key file can take everything!\n")
		fmt.Printf("You should start over and use a good passphrase!\n")
		fmt.Printf("Saved unencrypted key at %s\n", filename)
		return nil
	}

	salt := new([24]byte) // salt for scrypt / nonce for secretbox
	dk32 := new([32]byte) // derived key from scrypt

	//get 24 random bytes for scrypt salt (and secretbox nonce)
	_, err := rand.Read(salt[:])
	if err != nil {
		return err
	}

	// next use the pass and salt to make a 32-byte derived key
	dk, err := scrypt.Key(pass, salt[:], 16384, 8, 1, 32)
	if err != nil {
		return err
	}
	copy(dk32[:], dk[:])

	enckey := append(salt[:], secretbox.Seal(nil, priv[:], salt, dk32)...)
	//	enckey = append(salt, enckey...)
	keyhex := fmt.Sprintf("%x\n", enckey)

	err = ioutil.WriteFile(filename, []byte(keyhex), 0600)
	if err != nil {
		return err
	}
	fmt.Printf("Wrote encrypted key to %s\n", filename)
	return nil
}

// ReadKeyFile returns an 32 byte key from a file.
// If there's no file there, it'll make one.  If there's a password needed,
// it'll prompt for one.  One stop function.
func ReadKeyFile(filename string) (*[96]byte, error) {
	key := new([96]byte)
	_, err := os.Stat(filename)
	if err != nil {
		if os.IsNotExist(err) {
			// no key found, generate and save one
			fmt.Printf("No file %s, generating.\n", filename)

			_, err := rand.Read(key[:])
			if err != nil {
				return nil, err
			}

			err = SaveKeyToFileInteractive(filename, key)
			if err != nil {
				return nil, err
			}
		} else {
			// unknown error, crash
			fmt.Printf("unknown\n")
			return nil, err
		}
	}
	return LoadKeyFromFileInteractive(filename)
}
