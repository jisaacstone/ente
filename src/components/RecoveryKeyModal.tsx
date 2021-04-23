import React, { useEffect, useState } from 'react';
import { downloadAsFile } from 'utils/file';
import { getRecoveryKey } from 'utils/crypto';
import { setJustSignedUp } from 'utils/storage';
import constants from 'utils/strings/constants';
import MessageDialog from './MessageDialog';
import EnteSpinner from './EnteSpinner';

interface Props {
    show: boolean;
    onHide: () => void;
    somethingWentWrong: any;
}
function RecoveryKeyModal({ somethingWentWrong, ...props }: Props) {
    const [recoveryKey, setRecoveryKey] = useState(null);
    useEffect(() => {
        if (!props.show) {
            return;
        }
        const main = async () => {
            const recoveryKey = await getRecoveryKey();
            if (!recoveryKey) {
                somethingWentWrong();
                props.onHide();
            }
            setRecoveryKey(recoveryKey);
        };
        main();
    }, [props.show]);

    function onSaveClick() {
        downloadAsFile(constants.RECOVERY_KEY_FILENAME, recoveryKey);
        onClose();
    }
    function onClose() {
        props.onHide();
    }
    return (
        <MessageDialog
            show={props.show}
            onHide={onClose}
            attributes={{
                title: constants.DOWNLOAD_RECOVERY_KEY,
                close: {
                    text: constants.SAVE_LATER,
                    variant: 'danger',
                },
                staticBackdrop: true,
                proceed: {
                    text: constants.SAVE,
                    action: onSaveClick,
                    disabled: !recoveryKey,
                },
            }}
        >
            <p>{constants.RECOVERY_KEY_DESCRIPTION}</p>
            <div
                style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    background: '#1a1919',
                    height: '150px',
                    padding: '40px',
                    color: 'white',
                    margin: '20px 0',
                }}
            >
                {recoveryKey ? (
                    <div
                        style={{
                            wordWrap: 'break-word',
                            overflowWrap: 'break-word',
                            minWidth: '30%',
                        }}
                    >
                        {recoveryKey}
                    </div>
                ) : (
                    <EnteSpinner />
                )}
            </div>
            <p>{constants.KEY_NOT_STORED_DISCLAIMER}</p>
        </MessageDialog>
    );
}
export default RecoveryKeyModal;
