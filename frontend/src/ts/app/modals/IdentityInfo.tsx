import React from "react";
import { bytesToBase64, classNames } from "../../core/util";
import { Identity } from "../../../proto/data";
import { hideModal } from "../ModalStack";
import * as identities from "../../data/identities";
import { CardModal, CardModalTitle } from "../../components/Modal";
import { Button, ButtonColor, ButtonSize } from "../../components/Buttons";
import { alertUser, promptUser } from "./Confirm";

type IdentityInfoModalProps = {
    identity: Identity;
};

export class IdentityInfoModal extends React.Component<IdentityInfoModalProps> {
    render() {
        const id = this.props.identity;
        const publicKey = id.publicKey ? bytesToBase64(id.publicKey) : "";
        const hash = id.id ? bytesToBase64(id.id) : "";
        let content = (
            <div className="flex flex-col w-full justify-center items-center">
                <dl className="w-full">
                    <DescriptionListItem label="ID" value={hash} dark={true} />
                    <DescriptionListItem
                        label="Website"
                        value={id.website?.name}
                    />
                    <DescriptionListItem
                        label="User Name"
                        value={id.user?.displayName}
                        dark={true}
                    />
                    <DescriptionListItem label="Public Key" value={publicKey} />
                    <DescriptionListItem
                        label="Signature Counter"
                        value={id.signatureCounter?.toString()}
                        dark={true}
                    />
                </dl>
            </div>
        );
        let title = (
            <CardModalTitle
                title="Info"
                button={
                    <Button
                        text="Close"
                        onClick={this.onCancel_}
                        size={ButtonSize.SM}
                        color={ButtonColor.CLEAR}
                    />
                }
            />
        );
        let buttons = (
            <div className="flex w-full justify-end gap-3 px-4 py-4 sm:px-6">
                <Button
                    text="Export"
                    onClick={this.export_}
                    color={ButtonColor.SECONDARY}
                />
                <Button
                    text="Delete"
                    onClick={this.delete_}
                    color={ButtonColor.ERROR}
                />
            </div>
        );
        return (
            <CardModal>
                {title}
                {content}
                {buttons}
            </CardModal>
        );
    }

    export_ = async () => {
        const id = this.props.identity.id;
        if (!id) {
            return;
        }
        const confirmed = await promptUser(
            "Warning: the exported file contains the unencrypted private key for " +
                "this passkey. Anyone who has the file can log in as this user. Only " +
                "share it with teammates over a trusted channel, and delete the file " +
                "once they have imported it. Do you want to export this passkey?",
            "Export Passkey"
        );
        if (!confirmed) {
            return;
        }
        const result = await identities.exportIdentity(id);
        if (result.canceled) {
            return;
        }
        await alertUser(
            result.message,
            result.ok ? "Passkey Exported" : "Export Failed"
        );
    };

    delete_ = async () => {
        const id = this.props.identity.id;
        if (id) {
            if (await identities.deleteIdentity(id)) {
                hideModal();
            }
        }
    };

    onCancel_ = () => {
        hideModal();
    };
}

class DescriptionListItem extends React.Component<{
    label?: string;
    value?: string;
    dark?: boolean;
}> {
    render() {
        return (
            <div
                className={classNames("px-4 py-3 grid grid-cols-3 gap-4", {
                    "bg-gray-50": !!this.props.dark,
                    "bg-white": !this.props.dark,
                })}
            >
                <dt className="text-sm font-medium text-gray-500">
                    {this.props.label}
                </dt>
                <dd className="mt-1 text-sm text-gray-900 col-span-2 mt-0 text-ellipsis overflow-hidden">
                    {this.props.value}
                </dd>
            </div>
        );
    }
}
