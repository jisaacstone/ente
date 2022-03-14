import * as HeicConvert from 'heic-convert';

const WAIT_TIME_IN_MICROSECONDS = 10 * 1000;
const BREATH_TIME_IN_MICROSECONDS = 1000;

export async function convertHEIC(
    fileBlob: Blob,
    format: string
): Promise<Blob> {
    return await new Promise((resolve, reject) => {
        const main = async () => {
            try {
                const filedata = new Uint8Array(await fileBlob.arrayBuffer());
                const timeout = setTimeout(() => {
                    reject(Error('wait time exceeded'));
                }, WAIT_TIME_IN_MICROSECONDS);
                const result = await HeicConvert({ buffer: filedata, format });
                clearTimeout(timeout);
                const convertedFileData = new Uint8Array(result);
                const convertedFileBlob = new Blob([convertedFileData]);
                await new Promise((resolve) => {
                    setTimeout(
                        () => resolve(null),
                        BREATH_TIME_IN_MICROSECONDS
                    );
                });
                resolve(convertedFileBlob);
            } catch (e) {
                reject(e);
            }
        };
        main();
    });
}
