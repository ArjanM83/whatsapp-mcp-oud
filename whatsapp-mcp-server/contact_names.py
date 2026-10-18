"""Names for WhatsApp person IDs, read live from the bridge's whatsmeow store.

WhatsApp addresses most people by a privacy LID (12345678901234@lid), most
visibly as the sender in group chats and in @-mentions. messages.db does not
know who that is, but the bridge's whatsapp.db does, along two routes:

    <lid>@lid -> whatsmeow_lid_map:  lid -> pn (phone number)
              -> whatsmeow_contacts: pn@s.whatsapp.net -> name
    <lid>@lid -> whatsmeow_contacts: <lid>@lid -> name

The address-book name (full_name, then first_name) wins over the push name
the contact set themselves, whichever route it comes from. business_name is
the last resort: business accounts have empty personal fields. This is the
same order the bridge uses for chat names (whatsapp-bridge/names.go).

The database is opened read-only, so this can never harm the bridge.
"""

import os.path
import re
import sqlite3
import time
from typing import Dict, List, Optional, Tuple

WHATSAPP_DB_PATH = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'whatsapp-bridge', 'store', 'whatsapp.db')

LID = "lid"
PN = "s.whatsapp.net"

# Seconds a loaded snapshot is reused; new contacts show up after this.
CACHE_SECONDS = 30

MENTION_RE = re.compile(r"@(\d{9,})")


def split_jid(jid: str) -> Tuple[str, Optional[str]]:
    """'316..:44@s.whatsapp.net' -> ('316..', 's.whatsapp.net'); a bare ID has no server."""
    user, _, server = str(jid).strip().partition("@")
    return user.split(":")[0], (server or None)


class ContactNames:
    def __init__(self, db_path: str = WHATSAPP_DB_PATH):
        self.lid_to_pn: Dict[str, str] = {}
        self.pn_to_lid: Dict[str, str] = {}
        # (user, server) -> (full_name, first_name, push_name, business_name)
        self.contacts: Dict[Tuple[str, str], Tuple[str, ...]] = {}
        if not os.path.exists(db_path):
            return
        conn = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
        try:
            for lid, pn in conn.execute("SELECT lid, pn FROM whatsmeow_lid_map"):
                lid, pn = split_jid(lid)[0], split_jid(pn)[0]
                self.lid_to_pn[lid] = pn
                self.pn_to_lid[pn] = lid
            for their_jid, full, first, push, business in conn.execute(
                "SELECT their_jid, full_name, first_name, push_name, business_name FROM whatsmeow_contacts"
            ):
                user, server = split_jid(their_jid)
                fields = tuple((f or "").strip() for f in (full, first, push, business))
                if any(fields):
                    self.contacts[(user, server)] = fields
        finally:
            conn.close()

    def _ids(self, jid: str) -> Tuple[Optional[str], Optional[str]]:
        """(phone number, LID) of a person, as far as known."""
        user, server = split_jid(jid)
        if not user.isdigit():
            return None, None
        if server is None:
            # A bare ID can be either; a known LID is the likelier reading.
            server = LID if (user in self.lid_to_pn or (user, LID) in self.contacts) else PN
        if server == LID:
            return self.lid_to_pn.get(user), user
        if server == PN:
            return user, self.pn_to_lid.get(user)
        return None, None

    def person(self, jid: str) -> Tuple[Optional[str], Optional[str], Optional[str]]:
        """(name, phone number, LID) for a person JID or bare ID; unknown parts are None."""
        pn, lid = self._ids(jid)
        rows = [self.contacts[key] for key in ((pn, PN), (lid, LID)) if key[0] and key in self.contacts]
        for i in range(4):
            for row in rows:
                if row[i]:
                    return row[i], pn, lid
        return None, pn, lid

    def name(self, jid: str) -> Optional[str]:
        return self.person(jid)[0]

    def people(self) -> List[Tuple[Optional[str], Optional[str], Optional[str]]]:
        """Every person in the store, once each, as (name, phone number, LID)."""
        seen = set()
        result = []
        for user, server in list(self.contacts) + [(lid, LID) for lid in self.lid_to_pn]:
            name, pn, lid = self.person(f"{user}@{server}")
            key = pn or lid
            if key and key not in seen:
                seen.add(key)
                result.append((name, pn, lid))
        return result

    def replace_mentions(self, text: Optional[str]) -> Optional[str]:
        """'@12345678901234' in message text -> '@Name' where the ID is known."""
        if not text:
            return text

        def repl(m):
            name = self.name(m.group(1))
            return f"@{name}" if name else m.group(0)

        return MENTION_RE.sub(repl, text)


_cache: Dict[str, object] = {"at": 0.0, "names": None}


def contact_names() -> ContactNames:
    """A recent ContactNames snapshot (reloaded after CACHE_SECONDS)."""
    now = time.monotonic()
    if _cache["names"] is None or now - _cache["at"] > CACHE_SECONDS:
        try:
            _cache["names"] = ContactNames()
        except sqlite3.Error as e:
            print(f"Database error while loading contact names: {e}")
            _cache["names"] = _cache["names"] or ContactNames(db_path="")
        _cache["at"] = now
    return _cache["names"]
