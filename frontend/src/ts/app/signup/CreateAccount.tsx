import React from "react";
import {
    LOCAL_VAULT_TYPE
} from "../../data/passphrase";
import { createLocalVault } from "./NewVault";
import { hideModal, showModal } from "../ModalStack";
import { Button, ButtonColor, ButtonSize } from "../../components/Buttons";

export async function createNewVault(): Promise<[string, boolean]> {
    return new Promise((resolve) => {
        showModal(
            <CreateAccount
                onCreated={(type: string) => {
                    hideModal();
                    resolve([type, false]);
                }}
            />
        );
    });
}

type CreateAccountProps = {
    onCreated: (type: string) => void;
};

type CreateAccountState = {
    errorMessage?: string;
};

export class CreateAccount extends React.Component<
    CreateAccountProps,
    CreateAccountState
> {
    state: CreateAccountState = {};
    render() {
        let errorMessage;
        if (this.state.errorMessage) {
            errorMessage = (
                <div className="text-red-500 font-bold text-center mb-4">
                    {this.state.errorMessage}
                </div>
            );
        }
        const bottomButtons = (
            <div className="flex flex-col items-center mb-4 space-y-1">
                <Button
                    text="Use Local-Only Vault"
                    onClick={this.onUseLocal_}
                    size={ButtonSize.SM}
                    color={ButtonColor.CLEAR}
                />
            </div>
        );
        return (
            <div className="flex flex-col bg-gray-200 min-h-full">
                {bottomButtons}
            </div>
        );
    }

    onUseLocal_ = async () => {
        if (await createLocalVault()) {
            this.props.onCreated(LOCAL_VAULT_TYPE);
        }
    };
}
