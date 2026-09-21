import { callRPC } from "../core/rpc";
import { base64ToBytes, bytesToBase64, setImmediate } from "../core/util";
import { Identity } from "../../proto/data";
import { LogDebug } from "../wailsjs/runtime/runtime";

let identities: Identity[] = [];

let updateCallbackIndex = 0;
let updateCallbacks = new Map<number, (identities: Identity[]) => void>();

export function listenForUpdate(
    callback: (identities: Identity[]) => void
): number {
    updateCallbackIndex++;
    updateCallbacks.set(updateCallbackIndex, callback);
    setImmediate(() => {
        callback(identities);
    });
    return updateCallbackIndex;
}

export function unlistenForUpdate(index: number) {
    updateCallbacks.delete(index);
}

export async function update() {
    LogDebug("Updating identities: " + updateCallbacks);
    identities = await getIdentities();
    updateCallbacks.forEach((callback) => {
        callback(identities);
    });
}

export async function getIdentities(): Promise<Identity[]> {
    const protosRaw = (await callRPC("getIdentities")) as string[];
    const identities = [];
    for (const protoRaw of protosRaw) {
        const protoBytes = base64ToBytes(protoRaw); // Wails events converts bytes to base64
        const id = Identity.fromBinary(protoBytes, {
            readUnknownField: "throw",
        });
        identities.push(id);
    }
    return identities;
}

export async function deleteIdentity(id: Uint8Array) {
    return await callRPC("deleteIdentity", bytesToBase64(id));
}

// Result of a key-level export/import. `canceled` means the user dismissed the
// file dialog, in which case `message` is empty and nothing should be shown.
export type ShareResult = {
    ok: boolean;
    canceled: boolean;
    message: string;
};

export async function exportIdentity(id: Uint8Array): Promise<ShareResult> {
    return (await callRPC("exportIdentity", bytesToBase64(id))) as ShareResult;
}

export async function importIdentity(): Promise<ShareResult> {
    return (await callRPC("importIdentity")) as ShareResult;
}

export async function getFavicon(domain: string): Promise<string | null> {
    return await callRPC("getFavicon", domain);
}

// Relying parties are not required to send `rp.name` or `user.displayName` over
// CTAP, and browsers routinely omit them (notably for non-discoverable
// credentials) to avoid handing personal data to the authenticator. `rp.id` and
// `user.name` are the fields that are reliably present, so fall back to them
// instead of rendering a blank label.
export function websiteLabel(identity: Identity): string {
    return identity.website?.name || identity.website?.id || "Unknown website";
}

export function userLabel(identity: Identity): string {
    return identity.user?.displayName || identity.user?.name || "Unknown user";
}
