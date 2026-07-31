// connections_ui.go — Connections manager (File menu / F9 popup / each
// pane's own 🖥 toolbar button): saved remote connections (SFTP and SMB),
// backed by internal/connections (non-secret fields, plain JSON, mirroring
// favorites/editors) plus its keychain wrapper (the one stored secret per
// connection — see internal/connections/keychain.go). Connecting opens a
// NEW tab rooted at that connection's remote path — a deliberate, separate
// action, not an in-place swap the way opening an archive/listbox is (see
// fileListView.enterZip/enterListbox); matches how Search's "pick a result"
// opens a new tab too.
package main

import (
	"fmt"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"commander/internal/connections"
	"commander/internal/panelstate"
	"commander/internal/vfs/fileagentfs"
	"commander/internal/vfs/sftpfs"
	"commander/internal/vfs/smbfs"
)

// connectionDefaultPort is each protocol's conventional port, both for a
// blank/default port field and for deciding whether the user has
// customized it (see showConnectionForm's protocol-switch handler).
var connectionDefaultPort = map[string]string{"sftp": "22", "smb": "445", "fileagent": "9742"}

// isConnectionDefaultPort reports whether s is ANY protocol's own default —
// used to decide whether switching Protocol should update the Port field
// (still looks like an untouched default) or leave it alone (the user
// typed something else deliberately).
func isConnectionDefaultPort(s string) bool {
	for _, v := range connectionDefaultPort {
		if s == v {
			return true
		}
	}
	return false
}

// protocolDisplay/protocolFromDisplay map a Connection.Protocol value to
// and from the Protocol selector's own display strings — a real mapping
// rather than blind strings.ToUpper/ToLower, since "FileAgent" isn't its
// own uppercase form the way "SFTP"/"SMB" are.
var protocolDisplay = map[string]string{"sftp": "SFTP", "smb": "SMB", "fileagent": "FileAgent"}
var protocolFromDisplay = map[string]string{"SFTP": "sftp", "SMB": "smb", "FileAgent": "fileagent"}

func (c *commander) connectionsPath() string {
	p, err := connections.DefaultPath(appName)
	if err != nil {
		return ""
	}
	return p
}

func (c *commander) loadConnections() {
	path := c.connectionsPath()
	if path == "" {
		return
	}
	if cfg, err := connections.Load(path); err == nil {
		c.connectionConfig = cfg
	}
}

func (c *commander) saveConnections() {
	path := c.connectionsPath()
	if path == "" {
		return
	}
	_ = connections.Save(path, c.connectionConfig)
}

// showConnections lists saved connections (Connect/Edit/Remove per row) —
// "Add Connection…" and "Edit" both open showConnectionForm as a separate
// dialog rather than an always-visible inline form, so the list itself gets
// the dialog's full height (container.NewBorder's center stretches to fill
// it; a plain VBox of [scroll list, separator, an 8-field form] gave the
// list almost none of the height, since VBox never distributes leftover
// space — each child just gets its own natural minimum size).
// target is which pane "Connect" opens the new tab in.
func (c *commander) showConnections(target *pane) {
	list := container.NewVBox()
	var d dialog.Dialog
	var refresh func()
	var selectedID string

	refresh = func() {
		var rows []fyne.CanvasObject
		if len(c.connectionConfig.Connections) == 0 {
			rows = append(rows, widget.NewLabel("No saved connections yet."))
		}
		for _, conn := range c.connectionConfig.Connections {
			id := conn.ID
			connectBtn := widget.NewButton("Connect", func() {
				c.connectTo(conn, target, func() { d.Hide() }, func(err error) {
					// A failed connect very often means saved credentials
					// have gone stale (e.g. a FileAgent listener that was
					// restarted, generating a new PSK/TLS pin) — go straight
					// to editing this connection, with the error shown right
					// there, instead of a dead-end error dialog the user
					// would then have to close before finding Edit
					// themselves.
					c.showConnectionForm(&conn, err.Error(), refresh)
				})
			})
			editBtn := widget.NewButton("Edit", func() {
				c.showConnectionForm(&conn, "", refresh)
			})
			removeBtn := widget.NewButton("Remove", func() {
				c.connectionConfig.Remove(conn.ID)
				c.saveConnections()
				_ = connections.DeleteSecret(conn.ID)
				refresh()
			})
			// Clicking the name itself just selects/highlights it — no other
			// effect of its own — so a click that's about to be followed by
			// Connect/Edit/Remove has an obvious "yes, this is the one I
			// meant" confirmation first, rather than acting immediately on
			// whichever row happened to be clicked.
			protoLabel := protocolDisplay[conn.Protocol]
			if protoLabel == "" {
				protoLabel = conn.Protocol
			}
			who := conn.Username + "@"
			if conn.Username == "" { // fileagent has no username at all
				who = ""
			}
			label := newTappableLabel(
				fmt.Sprintf("%s  —  %s (%s)  %s%s:%d", conn.Name, protoLabel, conn.RemotePath, who, conn.Host, conn.Port),
				func() { selectedID = id; refresh() },
			)
			if id == selectedID {
				label.Importance = widget.HighImportance
			}
			row := container.NewBorder(nil, nil, nil, container.NewHBox(connectBtn, editBtn, removeBtn), label)
			rows = append(rows, row)
		}
		list.Objects = rows
		list.Refresh()
	}
	refresh()

	addBtn := widget.NewButton("Add Connection…", func() {
		c.showConnectionForm(nil, "", refresh)
	})

	content := container.NewBorder(nil, addBtn, nil, nil, container.NewVScroll(list))
	d = dialog.NewCustom("Connections", "Close", content, c.win)
	d.Resize(fyne.NewSize(620, 420))
	showDialog(d)
}

// showConnectionForm is Add (existing == nil) or Edit (existing != nil), as
// its own dialog on top of showConnections' list — Save persists and closes
// just this dialog, then calls onSaved so the list refreshes to show the
// change. The secret field is always blank on open (never pre-filled from
// the keychain, even when editing) — left blank on Save, an existing
// connection's stored secret is kept as-is rather than cleared.
//
// errMsg, if non-empty, shows as a banner at the top of the form instead of
// a separate error dialog — used when a Connect attempt just failed (see
// showConnections' connectBtn) so the likely fix (edit the stale
// password/passphrase/pre-shared-key/pin) is right there instead of behind
// a second "now go click Edit yourself" step. A plain Add/Edit from the
// list itself always passes "".
func (c *commander) showConnectionForm(existing *connections.Connection, errMsg string, onSaved func()) {
	nameEntry := newDialogEntry()
	nameEntry.SetPlaceHolder("Name (e.g. Home Server)")
	hostEntry := newDialogEntry()
	hostEntry.SetPlaceHolder("Host")
	portEntry := newDialogEntry()
	userEntry := newDialogEntry()
	userEntry.SetPlaceHolder("Username")
	remotePathLabel := widget.NewLabel("Remote Path:")
	remotePathEntry := newDialogEntry()
	keyPathEntry := newDialogEntry()
	keyPathEntry.SetPlaceHolder("Leave blank to use a password instead")
	domainEntry := newDialogEntry()
	domainEntry.SetPlaceHolder("Optional — NTLM domain/workgroup")
	tlsPinEntry := newDialogEntry()
	tlsPinEntry.SetPlaceHolder("SHA-256 fingerprint shown by the FileAgent listener on startup")
	secretEntry := newDialogEntry()
	secretEntry.Password = true

	usernameRow := container.NewGridWithColumns(2, widget.NewLabel("Username:"), userEntry)
	sshKeyRow := container.NewGridWithColumns(2, widget.NewLabel("SSH Key Path:"), container.NewBorder(nil, nil, nil,
		widget.NewButton("Browse…", func() {
			fd := dialog.NewFileOpen(func(uc fyne.URIReadCloser, err error) {
				if err != nil || uc == nil {
					return
				}
				defer uc.Close()
				keyPathEntry.SetText(uc.URI().Path())
			}, c.win)
			if home, err := c.fs.HomeDir(); err == nil && home != "" {
				if uri := storage.NewFileURI(home); uri != nil {
					if lister, err := storage.ListerForURI(uri); err == nil {
						fd.SetLocation(lister)
					}
				}
			}
			showDialog(fd)
		}),
		keyPathEntry))
	domainRow := container.NewGridWithColumns(2, widget.NewLabel("Domain:"), domainEntry)
	tlsPinRow := container.NewGridWithColumns(2, widget.NewLabel("TLS Cert Pin:"), tlsPinEntry)

	protocolSelect := widget.NewSelect([]string{"SFTP", "SMB", "FileAgent"}, nil)
	applyProtocol := func(display string) {
		switch display {
		case "SMB":
			usernameRow.Show()
			sshKeyRow.Hide()
			domainRow.Show()
			tlsPinRow.Hide()
			remotePathLabel.SetText("Share/Path:")
			remotePathEntry.SetPlaceHolder(`e.g. "Users" or "Users/allan"`)
			secretEntry.SetPlaceHolder("Password")
		case "FileAgent":
			usernameRow.Hide() // the protocol has no username at all — PSK-only auth
			sshKeyRow.Hide()
			domainRow.Hide()
			tlsPinRow.Show()
			remotePathLabel.SetText("Remote Path:")
			remotePathEntry.SetPlaceHolder("/ (the agent's own shared root)")
			secretEntry.SetPlaceHolder("Pre-Shared Key")
		default: // SFTP
			usernameRow.Show()
			sshKeyRow.Show()
			domainRow.Hide()
			tlsPinRow.Hide()
			remotePathLabel.SetText("Remote Path:")
			remotePathEntry.SetPlaceHolder("")
			secretEntry.SetPlaceHolder("Password, or the SSH key's passphrase if it has one")
		}
		if existing == nil { // don't fight the user's own typed port while editing
			if portEntry.Text == "" || isConnectionDefaultPort(portEntry.Text) {
				portEntry.SetText(connectionDefaultPort[protocolFromDisplay[display]])
			}
		}
	}
	protocolSelect.OnChanged = applyProtocol

	title := "Add Connection"
	id := connections.NewID()
	trustedFingerprint := ""
	protocol := "sftp"
	if existing != nil {
		title = "Edit Connection"
		id = existing.ID
		trustedFingerprint = existing.TrustedHostKeyFingerprint
		protocol = existing.Protocol
		nameEntry.SetText(existing.Name)
		hostEntry.SetText(existing.Host)
		if existing.Port != 0 {
			portEntry.SetText(strconv.Itoa(existing.Port))
		}
		userEntry.SetText(existing.Username)
		remotePathEntry.SetText(existing.RemotePath)
		keyPathEntry.SetText(existing.SSHKeyPath)
		domainEntry.SetText(existing.Domain)
		tlsPinEntry.SetText(existing.FileAgentTLSPin)
		secretEntry.SetPlaceHolder("Leave blank to keep the saved password/passphrase")
	}
	if portEntry.Text == "" {
		portEntry.SetText(connectionDefaultPort[protocol])
	}
	display := protocolDisplay[protocol]
	protocolSelect.SetSelected(display)
	applyProtocol(display) // SetSelected only fires OnChanged if the value actually changes

	fields := []fyne.CanvasObject{
		container.NewGridWithColumns(2, widget.NewLabel("Name:"), nameEntry),
		container.NewGridWithColumns(2, widget.NewLabel("Protocol:"), protocolSelect),
		container.NewGridWithColumns(2, widget.NewLabel("Host:"), hostEntry),
		container.NewGridWithColumns(2, widget.NewLabel("Port:"), portEntry),
		usernameRow,
		container.NewGridWithColumns(2, remotePathLabel, remotePathEntry),
		sshKeyRow,
		domainRow,
		tlsPinRow,
		container.NewGridWithColumns(2, widget.NewLabel("Password / Passphrase:"), secretEntry),
	}
	if errMsg != "" {
		warning := widget.NewLabel("Connect failed: " + errMsg)
		warning.Wrapping = fyne.TextWrapWord
		warning.Importance = widget.DangerImportance
		fields = append([]fyne.CanvasObject{warning, widget.NewSeparator()}, fields...)
	}
	content := container.NewVBox(fields...)

	d := dialog.NewCustomConfirm(title, "Save", "Cancel", content, func(ok bool) {
		if !ok {
			return
		}
		name := strings.TrimSpace(nameEntry.Text)
		host := strings.TrimSpace(hostEntry.Text)
		if name == "" || host == "" {
			return
		}
		proto := protocolFromDisplay[protocolSelect.Selected]
		port, err := strconv.Atoi(strings.TrimSpace(portEntry.Text))
		if err != nil || port <= 0 {
			port, _ = strconv.Atoi(connectionDefaultPort[proto])
		}
		fingerprint := trustedFingerprint
		if existing != nil && (proto != existing.Protocol || host != existing.Host || port != existing.Port) {
			// A different protocol/host/port invalidates any previously
			// trusted fingerprint — it belonged to the OLD endpoint.
			// Clearing it means the next connect gets a calm "new host"
			// trust prompt rather than a scary (and wrong) "this key
			// changed" one.
			fingerprint = ""
		}
		conn := connections.Connection{
			ID:                        id,
			Name:                      name,
			Protocol:                  proto,
			Host:                      host,
			Port:                      port,
			Username:                  strings.TrimSpace(userEntry.Text),
			RemotePath:                strings.TrimSpace(remotePathEntry.Text),
			SSHKeyPath:                strings.TrimSpace(keyPathEntry.Text),
			Domain:                    strings.TrimSpace(domainEntry.Text),
			TrustedHostKeyFingerprint: fingerprint,
			FileAgentTLSPin:           strings.TrimSpace(tlsPinEntry.Text),
		}
		if secretEntry.Text != "" {
			_ = connections.SetSecret(conn.ID, secretEntry.Text)
		}
		c.connectionConfig.Upsert(conn)
		c.saveConnections()
		if onSaved != nil {
			onSaved()
		}
	}, c.win)
	d.Resize(fyne.NewSize(560, 480))
	showDialog(d)
	c.win.Canvas().Focus(nameEntry)
}

// connectTo dispatches to this connection's own protocol. onConnected fires
// once the new tab is actually open (e.g. hiding showConnections' list
// dialog, so it's obvious something happened rather than just sitting
// there unchanged). onFailed fires instead of a bare error dialog on any
// failure — see showConnections' connectBtn, which uses it to jump straight
// to editing this connection with the error shown inline (stale saved
// credentials being the single most common cause of a failed connect).
func (c *commander) connectTo(conn connections.Connection, target *pane, onConnected func(), onFailed func(error)) {
	secret, _ := connections.GetSecret(conn.ID)
	switch conn.Protocol {
	case "smb":
		c.connectSMB(&conn, secret, target, onConnected, onFailed)
	case "fileagent":
		c.connectFileAgent(&conn, secret, target, onConnected, onFailed)
	default:
		c.connectSFTP(&conn, secret, target, onConnected, onFailed)
	}
}

// connectSFTP probes conn's host key in the background, then either
// connects straight away (already-trusted fingerprint) or shows a trust
// prompt first — see showHostKeyTrustPrompt and sftpfs's package doc for
// why this is a separate probe step rather than a prompt from inside the
// ssh library's own synchronous handshake callback. SMB has no equivalent
// step (see connectSMB) — NTLM auth has no host-identity primitive at this
// level the way SSH's host keys do.
func (c *commander) connectSFTP(conn *connections.Connection, secret string, target *pane, onConnected func(), onFailed func(error)) {
	go func() {
		fingerprint, err := sftpfs.ProbeHostKey(conn.Host, conn.Port)
		if err != nil {
			fyne.Do(func() { onFailed(fmt.Errorf("connect to %s: %w", conn.Host, err)) })
			return
		}
		if fingerprint == conn.TrustedHostKeyFingerprint {
			c.finishSFTPConnect(conn, secret, target, onConnected, onFailed)
			return
		}
		fyne.Do(func() { c.showHostKeyTrustPrompt(conn, secret, fingerprint, target, onConnected, onFailed) })
	}()
}

// showHostKeyTrustPrompt asks the user to confirm a host key fingerprint
// before ever relying on it — either the connection's first time (no
// fingerprint on file yet) or because it's genuinely changed since a prior
// trust, worded more alarmingly since that case could mean a real
// man-in-the-middle rather than just "haven't connected before." Accepting
// persists the fingerprint before connecting.
func (c *commander) showHostKeyTrustPrompt(conn *connections.Connection, secret, fingerprint string, target *pane, onConnected func(), onFailed func(error)) {
	title := "New Host Key"
	msg := fmt.Sprintf("%s (%s) presents a host key you haven't trusted before:\n\n%s\n\nTrust it and continue?", conn.Name, conn.Host, fingerprint)
	if conn.TrustedHostKeyFingerprint != "" {
		title = "Host Key Changed"
		msg = fmt.Sprintf(
			"WARNING: %s (%s)'s host key has CHANGED since you last trusted it — this could mean the server was reinstalled, or that something is impersonating it.\n\nPreviously trusted:\n%s\n\nNow presenting:\n%s\n\nTrust the new key and continue?",
			conn.Name, conn.Host, conn.TrustedHostKeyFingerprint, fingerprint)
	}
	showDialog(dialog.NewConfirm(title, msg, func(ok bool) {
		if !ok {
			return
		}
		conn.TrustedHostKeyFingerprint = fingerprint
		c.connectionConfig.Upsert(*conn)
		c.saveConnections()
		c.finishSFTPConnect(conn, secret, target, onConnected, onFailed)
	}, c.win))
}

// finishSFTPConnect does the real (authenticated) connect in the background
// and, on success, opens a new tab in target rooted at conn.RemotePath.
func (c *commander) finishSFTPConnect(conn *connections.Connection, secret string, target *pane, onConnected func(), onFailed func(error)) {
	go func() {
		fs, err := sftpfs.Connect(conn, secret)
		fyne.Do(func() {
			if err != nil {
				onFailed(fmt.Errorf("connect to %s: %w", conn.Host, err))
				return
			}
			state := panelstate.New(fs.Presented(conn.RemotePath))
			state.TabTitle = conn.Name
			target.addTabFromStateWithFS(state, fs)
			if onConnected != nil {
				onConnected()
			}
		})
	}()
}

// connectSMB connects and mounts conn's configured share in the background
// and, on success, opens a new tab in target at the parsed starting path
// (see smbfs.Connect/StartPath — conn.RemotePath's first component names
// the share itself, so there's no separate presented-path assembly step
// the way connectSFTP/finishSFTPConnect have).
func (c *commander) connectSMB(conn *connections.Connection, secret string, target *pane, onConnected func(), onFailed func(error)) {
	go func() {
		fs, err := smbfs.Connect(conn, secret)
		fyne.Do(func() {
			if err != nil {
				onFailed(fmt.Errorf("connect to %s: %w", conn.Host, err))
				return
			}
			state := panelstate.New(fs.StartPath())
			state.TabTitle = conn.Name
			target.addTabFromStateWithFS(state, fs)
			if onConnected != nil {
				onConnected()
			}
		})
	}()
}

// connectFileAgent connects to conn's FileAgent listener in the background
// and, on success, opens a new tab in target rooted at conn.RemotePath.
// Unlike SFTP, there's no separate probe/trust step: the TLS certificate
// pin is expected to already be known (copied from wherever the listener
// printed it on startup) and is checked directly inside fileagentfs.Connect.
func (c *commander) connectFileAgent(conn *connections.Connection, secret string, target *pane, onConnected func(), onFailed func(error)) {
	go func() {
		fs, err := fileagentfs.Connect(conn, secret)
		fyne.Do(func() {
			if err != nil {
				onFailed(fmt.Errorf("connect to %s: %w", conn.Host, err))
				return
			}
			state := panelstate.New(fs.Presented(conn.RemotePath))
			state.TabTitle = conn.Name
			target.addTabFromStateWithFS(state, fs)
			if onConnected != nil {
				onConnected()
			}
		})
	}()
}

// tappableLabel is a widget.Label that reports plain taps — used by
// showConnections so clicking a connection's name selects/highlights it
// (widget.HighImportance) with no other effect of its own; Connect/Edit/
// Remove stay exactly the per-row buttons they already were.
type tappableLabel struct {
	widget.Label
	onTap func()
}

func newTappableLabel(text string, onTap func()) *tappableLabel {
	l := &tappableLabel{onTap: onTap}
	l.Text = text
	l.ExtendBaseWidget(l)
	return l
}

func (l *tappableLabel) Tapped(*fyne.PointEvent) {
	if l.onTap != nil {
		l.onTap()
	}
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
