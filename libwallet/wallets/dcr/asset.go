package dcr

import (
	"context"
	"sync"

	"github.com/asdine/storm"
	"github.com/decred/dcrd/chaincfg/v3"
	"gitlab.com/raedah/cryptopower/libwallet/internal/vsp"
	"gitlab.com/raedah/cryptopower/libwallet/wallets/wallet"
	"gitlab.com/raedah/cryptopower/libwallet/wallets/wallet/walletdata"
)

// To be renamed to DCRAsset when optimizing the code.
// type DCRAsset struct {
type Wallet struct {
	*wallet.Wallet

	ID int `storm:"id,increment"` // needed to existing wallets at the multiwallet

	rootDir string

	synced            bool
	syncing           bool
	waitingForHeaders bool

	chainParams  *chaincfg.Params
	walletDataDB *walletdata.DB

	cancelAccountMixer context.CancelFunc `json:"-"`

	cancelAutoTicketBuyerMu sync.Mutex
	cancelAutoTicketBuyer   context.CancelFunc `json:"-"`

	vspClientsMu sync.Mutex
	vspClients   map[string]*vsp.Client
	vspMu        sync.RWMutex
	vsps         []*wallet.VSP

	notificationListenersMu          sync.RWMutex
	syncData                         *SyncData
	accountMixerNotificationListener map[string]wallet.AccountMixerNotificationListener
	txAndBlockNotificationListeners  map[string]wallet.TxAndBlockNotificationListener
	blocksRescanProgressListener     wallet.BlocksRescanProgressListener
}

func CreateNewWallet(walletName, privatePassphrase string, privatePassphraseType int32, db *storm.DB, rootDir, dbDriver string, chainParams *chaincfg.Params) (*Wallet, error) {
	w, err := wallet.CreateNewWallet(walletName, privatePassphrase, privatePassphraseType, db, rootDir, dbDriver, chainParams)
	if err != nil {
		return nil, err
	}

	dcrWallet := &Wallet{
		Wallet: w,

		rootDir:     rootDir, // To moved to the upstream wallet
		chainParams: chainParams,

		syncData: &SyncData{
			syncProgressListeners: make(map[string]wallet.SyncProgressListener),
		},
		txAndBlockNotificationListeners:  make(map[string]wallet.TxAndBlockNotificationListener),
		accountMixerNotificationListener: make(map[string]wallet.AccountMixerNotificationListener),
		vspClients:                       make(map[string]*vsp.Client),
	}

	dcrWallet.SetNetworkCancelCallback(dcrWallet.SafelyCancelSync)

	return dcrWallet, nil
}

func CreateWatchOnlyWallet(walletName, extendedPublicKey string, db *storm.DB, rootDir, dbDriver string, chainParams *chaincfg.Params) (*Wallet, error) {
	w, err := wallet.CreateWatchOnlyWallet(walletName, extendedPublicKey, db, rootDir, dbDriver, chainParams)
	if err != nil {
		return nil, err
	}

	dcrWallet := &Wallet{
		Wallet: w,

		rootDir:     rootDir, // To moved to the upstream wallet
		chainParams: chainParams,

		syncData: &SyncData{
			syncProgressListeners: make(map[string]wallet.SyncProgressListener),
		},
	}

	dcrWallet.SetNetworkCancelCallback(dcrWallet.SafelyCancelSync)

	return dcrWallet, nil
}

func RestoreWallet(walletName, seedMnemonic, rootDir, dbDriver string, db *storm.DB, chainParams *chaincfg.Params, privatePassphrase string, privatePassphraseType int32) (*Wallet, error) {
	w, err := wallet.RestoreWallet(walletName, seedMnemonic, rootDir, dbDriver, db, chainParams, privatePassphrase, privatePassphraseType)
	if err != nil {
		return nil, err
	}

	dcrWallet := &Wallet{
		Wallet: w,

		rootDir:     rootDir, // To moved to the upstream wallet
		chainParams: chainParams,

		syncData: &SyncData{
			syncProgressListeners: make(map[string]wallet.SyncProgressListener),
		},
		vspClients: make(map[string]*vsp.Client),
	}

	dcrWallet.SetNetworkCancelCallback(dcrWallet.SafelyCancelSync)

	return dcrWallet, nil
}

func (wallet *Wallet) Synced() bool {
	return wallet.synced
}

func (wallet *Wallet) LockWallet() {
	if wallet.IsAccountMixerActive() {
		log.Error("LockWallet ignored due to active account mixer")
		return
	}

	if !wallet.Internal().Locked() {
		wallet.Internal().Lock()
	}
}

func (wallet *Wallet) SafelyCancelSync() {
	if wallet.IsConnectedToDecredNetwork() {
		wallet.CancelSync()
		defer func() {
			wallet.SpvSync()
		}()
	}
}
