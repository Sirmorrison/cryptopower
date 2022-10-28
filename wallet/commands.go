package wallet

import "code.cryptopower.dev/group/cryptopower/libwallet"

// TODO command.go file to be deprecated in subsiquent code clean up

// TODO move method to libwallet
// HaveAddress checks if the given address is valid for the wallet
func (wal *Wallet) HaveAddress(address string) (bool, string) {
	for _, wallet := range wal.multi.AllDCRWallets() {
		result := wallet.HaveAddress(address)
		if result {
			return true, wallet.GetWalletName()
		}
	}
	return false, ""
}

func (wal *Wallet) GetMultiWallet() *libwallet.AssetsManager {
	return wal.multi
}
