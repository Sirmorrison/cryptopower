package governance

import (
	"context"
	"fmt"
	"sync"

	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gitlab.com/raedah/cryptopower/libwallet"
	"gitlab.com/raedah/cryptopower/ui/cryptomaterial"
	"gitlab.com/raedah/cryptopower/ui/load"
	"gitlab.com/raedah/cryptopower/ui/modal"
	"gitlab.com/raedah/cryptopower/ui/page/components"
	"gitlab.com/raedah/cryptopower/ui/values"
)

type voteModal struct {
	*load.Load
	*cryptomaterial.Modal

	detailsMu      sync.Mutex
	detailsCancel  context.CancelFunc
	voteDetails    *libwallet.ProposalVoteDetails
	voteDetailsErr error

	proposal *libwallet.Proposal
	isVoting bool

	walletSelector *WalletSelector
	materialLoader material.LoaderStyle
	yesVote        *inputVoteOptionsWidgets
	noVote         *inputVoteOptionsWidgets
	voteBtn        cryptomaterial.Button
	cancelBtn      cryptomaterial.Button
}

func newVoteModal(l *load.Load, proposal *libwallet.Proposal) *voteModal {
	vm := &voteModal{
		Load:           l,
		Modal:          l.Theme.ModalFloatTitle("input_vote_modal"),
		proposal:       proposal,
		materialLoader: material.Loader(l.Theme.Base),
		voteBtn:        l.Theme.Button(values.String(values.StrVote)),
		cancelBtn:      l.Theme.OutlineButton(values.String(values.StrCancel)),
	}

	vm.yesVote = newInputVoteOptions(vm.Load, values.String(values.StrYes))
	vm.noVote = newInputVoteOptions(vm.Load, values.String(values.StrNo))
	vm.noVote.activeBg = l.Theme.Color.Orange2
	vm.noVote.dotColor = l.Theme.Color.Danger

	vm.walletSelector = NewWalletSelector(l).
		Title(values.String(values.StrVotingWallet)).
		WalletSelected(func(w *libwallet.Wallet) {

			vm.detailsMu.Lock()
			vm.yesVote.reset()
			vm.noVote.reset()
			// cancel current loading thread if any.
			if vm.detailsCancel != nil {
				vm.detailsCancel()
			}

			ctx, cancel := context.WithCancel(context.Background())
			vm.detailsCancel = cancel

			vm.voteDetails = nil
			vm.voteDetailsErr = nil

			vm.detailsMu.Unlock()

			vm.ParentWindow().Reload()

			go func() {
				voteDetails, err := vm.WL.MultiWallet.Politeia.ProposalVoteDetailsRaw(ctx, w.Internal(), vm.proposal.Token)
				vm.detailsMu.Lock()
				if !components.ContextDone(ctx) {
					vm.voteDetails = &libwallet.ProposalVoteDetails{ProposalVoteDetails: *voteDetails}
					vm.voteDetailsErr = err
				}
				vm.detailsMu.Unlock()
			}()
		}).
		WalletValidator(func(w *libwallet.Wallet) bool {
			return !w.IsWatchingOnlyWallet()
		})
	return vm
}

func (vm *voteModal) OnResume() {
	vm.walletSelector.SelectFirstValidWallet()
}

func (vm *voteModal) OnDismiss() {

}

func (vm *voteModal) eligibleVotes() int {
	vm.detailsMu.Lock()
	voteDetails := vm.voteDetails
	vm.detailsMu.Unlock()

	if voteDetails == nil {
		return 0
	}

	return len(voteDetails.EligibleTickets)
}

func (vm *voteModal) remainingVotes() int {
	vm.detailsMu.Lock()
	voteDetails := vm.voteDetails
	vm.detailsMu.Unlock()

	if voteDetails == nil {
		return 0
	}

	return len(voteDetails.EligibleTickets) - (vm.yesVote.voteCount() + vm.noVote.voteCount())
}

func (vm *voteModal) sendVotes() {
	vm.detailsMu.Lock()
	tickets := vm.voteDetails.EligibleTickets
	vm.detailsMu.Unlock()

	votes := make([]*libwallet.ProposalVote, 0)
	addVotes := func(bit string, count int) {
		for i := 0; i < count; i++ {

			// get and pop
			tickets = tickets[1:]

			vote := &libwallet.ProposalVote{}
			vote.Ticket.Hash = tickets[0].Hash
			vote.Ticket.Address = tickets[0].Address
			vote.Bit = bit

			votes = append(votes, vote)
		}
	}

	addVotes(libwallet.VoteBitYes, vm.yesVote.voteCount())
	addVotes(libwallet.VoteBitNo, vm.noVote.voteCount())

	ctx := context.Background()
	passwordModal := modal.NewCreatePasswordModal(vm.Load).
		EnableName(false).
		EnableConfirmPassword(false).
		Title(values.String(values.StrVoteConfirm)).
		SetNegativeButtonCallback(func() { vm.isVoting = false }).
		SetPositiveButtonCallback(func(_, password string, pm *modal.CreatePasswordModal) bool {
			isSuccess := true
			go func(isClosing *bool) {
				w := vm.walletSelector.selectedWallet.Internal()
				err := vm.WL.MultiWallet.Politeia.CastVotes(ctx, w, libwallet.ConvertVotes(votes), vm.proposal.Token, password)
				if err != nil {
					pm.SetError(err.Error())
					pm.SetLoading(false)
					*isClosing = false
					return
				}
				pm.Dismiss()
				infoModal := modal.NewSuccessModal(vm.Load, values.String(values.StrVoteSent), modal.DefaultClickFunc())
				vm.ParentWindow().ShowModal(infoModal)
				go vm.WL.MultiWallet.Politeia.Sync(ctx)
				vm.Dismiss()
			}(&isSuccess)

			return isSuccess
		})
	vm.ParentWindow().ShowModal(passwordModal)
}

func (vm *voteModal) Handle() {
	for vm.cancelBtn.Clicked() {
		if vm.isVoting {
			continue
		}
		vm.Dismiss()
	}

	vm.handleVoteCountButtons(vm.yesVote)
	vm.handleVoteCountButtons(vm.noVote)

	totalVotes := vm.yesVote.voteCount() + vm.noVote.voteCount()
	validToVote := totalVotes > 0 && totalVotes <= vm.eligibleVotes()
	vm.voteBtn.SetEnabled(validToVote)

	for vm.voteBtn.Clicked() {
		if vm.isVoting {
			break
		}

		if !validToVote {
			break
		}

		vm.isVoting = true
		vm.sendVotes()
	}
}

// - Layout

func (vm *voteModal) Layout(gtx layout.Context) D {
	vm.detailsMu.Lock()
	voteDetails := vm.voteDetails
	voteDetailsErr := vm.voteDetailsErr
	vm.detailsMu.Unlock()
	w := []layout.Widget{
		func(gtx C) D {
			t := vm.Theme.H6(values.String(values.StrVote))
			t.Font.Weight = text.SemiBold
			return t.Layout(gtx)
		},
		func(gtx C) D {
			return vm.walletSelector.Layout(gtx, vm.ParentWindow())
		},
		func(gtx C) D {
			if voteDetails != nil {
				return D{}
			}

			if voteDetailsErr != nil {
				return vm.Theme.Label(values.TextSize16, voteDetailsErr.Error()).Layout(gtx)
			}

			gtx.Constraints.Min.X = gtx.Dp(values.MarginPadding24)
			return vm.materialLoader.Layout(gtx)
		},
		func(gtx C) D {
			if voteDetails == nil {
				return D{}
			}

			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Bottom: values.MarginPadding16}.Layout(gtx, func(gtc C) D {
						return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if voteDetails.YesVotes == 0 {
									return layout.Dimensions{}
								}

								wrap := vm.Theme.Card()
								wrap.Color = vm.Theme.Color.Green50
								wrap.Radius = cryptomaterial.Radius(8)
								if voteDetails.NoVotes > 0 {
									wrap.Radius.TopRight = 0
									wrap.Radius.BottomRight = 0
								}
								return wrap.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									inset := layout.Inset{
										Left:   values.MarginPadding12,
										Top:    values.MarginPadding8,
										Right:  values.MarginPadding12,
										Bottom: values.MarginPadding8,
									}
									return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx C) D {
												card := vm.Theme.Card()
												card.Color = vm.Theme.Color.Green500
												card.Radius = cryptomaterial.Radius(4)
												return card.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													gtx.Constraints.Min.X += gtx.Dp(values.MarginPadding8)
													gtx.Constraints.Min.Y += gtx.Dp(values.MarginPadding8)
													return layout.Dimensions{Size: gtx.Constraints.Min}
												})
											}),
											layout.Rigid(func(gtx C) D {
												return layout.Inset{Left: values.MarginPadding4}.Layout(gtx, func(gtx C) D {
													label := vm.Theme.Body2(fmt.Sprintf("%s: %d", values.String(values.StrYes), voteDetails.YesVotes))
													return label.Layout(gtx)
												})
											}),
										)
									})
								})
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if voteDetails.NoVotes == 0 {
									return layout.Dimensions{}
								}

								wrap := vm.Theme.Card()
								wrap.Color = vm.Theme.Color.Orange2
								wrap.Radius = cryptomaterial.Radius(8)
								if voteDetails.YesVotes > 0 {
									wrap.Radius.TopLeft = 0
									wrap.Radius.BottomLeft = 0
								}
								return wrap.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									inset := layout.Inset{
										Left:   values.MarginPadding12,
										Top:    values.MarginPadding8,
										Right:  values.MarginPadding12,
										Bottom: values.MarginPadding8,
									}
									return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx C) D {
												card := vm.Theme.Card()
												card.Color = vm.Theme.Color.Danger
												card.Radius = cryptomaterial.Radius(4)
												return card.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													gtx.Constraints.Min.X += gtx.Dp(values.MarginPadding8)
													gtx.Constraints.Min.Y += gtx.Dp(values.MarginPadding8)
													return layout.Dimensions{Size: gtx.Constraints.Min}
												})
											}),
											layout.Rigid(func(gtx C) D {
												return layout.Inset{Left: values.MarginPadding4}.Layout(gtx, func(gtx C) D {
													label := vm.Theme.Body2(fmt.Sprintf("%s: %d", values.String(values.StrNo), voteDetails.NoVotes))
													return label.Layout(gtx)
												})
											}),
										)
									})
								})
							}),
						)
					})
				}),
				layout.Rigid(func(gtx C) D {
					if voteDetails == nil {
						return D{}
					}

					text := values.StringF(values.StrNumberOfVotes, len(voteDetails.EligibleTickets))
					return vm.Theme.Label(values.TextSize16, text).Layout(gtx)
				}),
				layout.Rigid(func(gtx C) D {
					return vm.inputOptions(gtx, vm.yesVote)
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{
						Top: values.MarginPadding8,
					}.Layout(gtx, func(gtx C) D {
						return vm.inputOptions(gtx, vm.noVote)
					})
				}),
			)
		},
		func(gtx C) D {
			if voteDetails != nil && vm.yesVote.voteCount()+vm.noVote.voteCount() > len(voteDetails.EligibleTickets) {
				label := vm.Theme.Label(values.TextSize14, values.String(values.StrNotEnoughVotes))
				label.Color = vm.Theme.Color.Danger
				return label.Layout(gtx)
			}

			return D{}
		},
		func(gtx C) D {
			return layout.E.Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Right: values.MarginPadding8}.Layout(gtx, vm.cancelBtn.Layout)
					}),
					layout.Rigid(func(gtx C) D {
						if vm.isVoting {
							return vm.materialLoader.Layout(gtx)
						}
						return vm.voteBtn.Layout(gtx)
					}),
				)
			})
		},
	}

	return vm.Modal.Layout(gtx, w)
}

func (vm *voteModal) inputOptions(gtx layout.Context, wdg *inputVoteOptionsWidgets) D {
	wrap := vm.Theme.Card()
	wrap.Color = vm.Theme.Color.Gray4
	dotColor := vm.Theme.Color.Gray3
	if wdg.voteCount() > 0 {
		wrap.Color = wdg.activeBg
		dotColor = wdg.dotColor
	}
	return wrap.Layout(gtx, func(gtx C) D {
		inset := layout.Inset{
			Top:    values.MarginPadding8,
			Bottom: values.MarginPadding8,
			Left:   values.MarginPadding16,
			Right:  values.MarginPadding8,
		}
		return inset.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(.4, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							card := vm.Theme.Card()
							card.Color = dotColor
							card.Radius = cryptomaterial.Radius(4)
							return card.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.X += gtx.Dp(values.MarginPadding8)
								gtx.Constraints.Min.Y += gtx.Dp(values.MarginPadding8)
								return layout.Dimensions{Size: gtx.Constraints.Min}
							})
						}),
						layout.Rigid(func(gtx C) D {
							return layout.Inset{Left: values.MarginPadding4}.Layout(gtx, func(gtx C) D {
								return vm.Theme.Body2(wdg.label).Layout(gtx)
							})
						}),
					)
				}),
				layout.Flexed(.6, func(gtx C) D {
					border := widget.Border{
						Color:        vm.Theme.Color.Gray2,
						CornerRadius: values.MarginPadding8,
						Width:        values.MarginPadding2,
					}

					return border.Layout(gtx, func(gtx C) D {
						card := vm.Theme.Card()
						card.Color = vm.Theme.Color.Surface
						return card.Layout(gtx, func(gtx C) D {
							var height int
							gtx.Constraints.Min.X = gtx.Constraints.Max.X
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Flexed(1, func(gtx C) D {
									dims := layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
										layout.Rigid(func(gtx C) D {
											return wdg.decrement.Layout(gtx)
										}),
										layout.Rigid(func(gtx C) D {
											gtx.Constraints.Min.X, gtx.Constraints.Max.X = 30, 30
											return wdg.input.Layout(gtx)
										}),
										layout.Rigid(func(gtx C) D {
											return wdg.increment.Layout(gtx)
										}),
									)
									height = dims.Size.Y
									return dims
								}),
								layout.Flexed(0.02, func(gtx C) D {
									line := vm.Theme.Line(height, gtx.Dp(values.MarginPadding2))
									line.Color = vm.Theme.Color.Gray2
									return line.Layout(gtx)
								}),
								layout.Rigid(func(gtx C) D {
									return wdg.max.Layout(gtx)
								}),
							)
						})
					})
				}),
			)
		})
	})
}
