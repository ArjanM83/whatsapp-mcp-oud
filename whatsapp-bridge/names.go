package main

import (
	"context"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// Contact names for 1-on-1 chats.
//
// WhatsApp now addresses most people by a privacy LID (12345@lid), while the
// address-book name is usually stored on the phone-number JID
// (316...@s.whatsapp.net). Looking a LID up directly therefore often finds
// nothing, or only the push name the contact chose for themselves, and the
// chat ended up named after a bare number. contactName follows the
// whatsmeow LID map to the phone-number JID as well and picks the best name
// from both rows.

// contactName returns the best known name for a person JID (@lid or
// @s.whatsapp.net), or "" when the store has none. The address-book name
// (full name, then first name) always wins over the push name, whichever JID
// it is stored on; business_name is the last resort for business accounts,
// whose personal fields are empty.
func contactName(ctx context.Context, client *whatsmeow.Client, jid types.JID) string {
	jid = jid.ToNonAD()
	candidates := []types.JID{jid}
	switch jid.Server {
	case types.HiddenUserServer:
		if pn, err := client.Store.LIDs.GetPNForLID(ctx, jid); err == nil && !pn.IsEmpty() {
			candidates = []types.JID{pn, jid}
		}
	case types.DefaultUserServer:
		if lid, err := client.Store.LIDs.GetLIDForPN(ctx, jid); err == nil && !lid.IsEmpty() {
			candidates = append(candidates, lid)
		}
	default:
		return ""
	}

	var infos []types.ContactInfo
	for _, c := range candidates {
		if info, err := client.Store.Contacts.GetContact(ctx, c); err == nil && info.Found {
			infos = append(infos, info)
		}
	}
	fields := []func(types.ContactInfo) string{
		func(i types.ContactInfo) string { return i.FullName },
		func(i types.ContactInfo) string { return i.FirstName },
		func(i types.ContactInfo) string { return i.PushName },
		func(i types.ContactInfo) string { return i.BusinessName },
	}
	for _, field := range fields {
		for _, info := range infos {
			if name := strings.TrimSpace(field(info)); name != "" {
				return name
			}
		}
	}
	return ""
}

// isPlaceholderName reports whether a stored chat name is only a stand-in
// (empty, or a bare number such as the chat's own LID or the sender's ID)
// rather than a real name worth keeping.
func isPlaceholderName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	for _, r := range name {
		if (r < '0' || r > '9') && r != '+' {
			return false
		}
	}
	return true
}

// repairChatNames gives every 1-on-1 chat the best name the store now knows.
// It replaces placeholder names and upgrades a push name to the address-book
// name once that becomes available. Group chats are left alone.
func repairChatNames(client *whatsmeow.Client, messageStore *MessageStore, logger waLog.Logger) {
	ctx := context.Background()
	rows, err := messageStore.db.Query(
		"SELECT jid, name FROM chats WHERE jid LIKE '%@lid' OR jid LIKE '%@s.whatsapp.net'")
	if err != nil {
		logger.Warnf("Chat name repair: %v", err)
		return
	}
	type update struct{ jid, name string }
	var updates []update
	for rows.Next() {
		var chatJID string
		var current *string
		if err := rows.Scan(&chatJID, &current); err != nil {
			continue
		}
		jid, err := types.ParseJID(chatJID)
		if err != nil {
			continue
		}
		name := contactName(ctx, client, jid)
		if name != "" && (current == nil || *current != name) {
			updates = append(updates, update{chatJID, name})
		}
	}
	rows.Close()

	for _, u := range updates {
		if _, err := messageStore.db.Exec("UPDATE chats SET name = ? WHERE jid = ?", u.name, u.jid); err != nil {
			logger.Warnf("Chat name repair: %s: %v", u.jid, err)
			continue
		}
		logger.Infof("Chat name repaired: %s -> %s", u.jid, u.name)
	}
	logger.Infof("Chat name repair: %d chat(s) updated", len(updates))
}

// startChatNameRepair runs repairChatNames now and then every hour, so chats
// with a contact that only became known later (a new contact, a LID mapping
// learned from a group) still get their name.
func startChatNameRepair(client *whatsmeow.Client, messageStore *MessageStore, logger waLog.Logger) {
	go func() {
		for {
			repairChatNames(client, messageStore, logger)
			time.Sleep(time.Hour)
		}
	}()
}
