import { ACCOUNT_VAULT_TYPE, LOCAL_VAULT_TYPE } from "../../data/passphrase";
import { unlockLocalVault } from "./Unlock";
import { LogError } from "../../wailsjs/runtime/runtime";



export async function logInToExistingVault(
    vaultType: string,
    vaultData: string,
    email: string
): Promise<boolean> {
    if (vaultType === LOCAL_VAULT_TYPE) {
        return await unlockLocalVault(vaultData);
    } else {
        LogError(
            "Invalid vault type: '" +
                vaultType +
                "'" +
                (ACCOUNT_VAULT_TYPE === vaultType) +
                vaultType.localeCompare(ACCOUNT_VAULT_TYPE)
        );
        return false;
    }
}
