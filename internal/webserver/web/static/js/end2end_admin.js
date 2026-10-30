function displayError(err) {
    document.getElementById("errordiv").style.display = "block";
    const errorMessageEl = document.getElementById("errormessage");

    errorMessageEl.innerText = "";
    const bold = document.createElement("b");
    bold.innerText = t("common.error") + ": ";
    const message = document.createTextNode(err.toString().replace(/^Error:/gi, ""));

    errorMessageEl.appendChild(bold);
    errorMessageEl.appendChild(message);

    console.error('Caught exception', err);
}

function checkIfE2EKeyIsSet() {
    if (!isE2EKeySet()) {
        window.location = './e2eSetup';
    } else {
        loadWasm(function() {
            let key = localStorage.getItem("e2ekey");
            let err = GokapiE2ESetCipher(key);
            if (err !== null) {
                displayError(err);
                return;
            }
            getE2EInfo();
            dropzoneObject.enable();
            document.getElementById("selectUploadFiles").disabled = false;
            document.getElementById("uploadBoxTitle").innerText = t("request.drop");
            document.getElementById("uploadBoxSubtitle").innerText = t("e2e.uploadHint");
        });
    }
}

function getE2EInfo() {
    apiE2eMutexLockUnlock(false)
        .then(() => apiE2eGet())
        .then(data => {
            let err = GokapiE2EInfoParse(data);
            if (err === null) {
                GokapiE2EDecryptMenu();
            } else {
                displayError(err);
                if (err.message === "cipher: message authentication failed") {
                    invalidCipherRedirectConfim();
                } else {
                    displayError(t("e2e.getFailed") + data);
                }
            }
        })
        .catch(error => {
            displayError(t("e2e.getFailed") + error);
        })
        .finally(() => {
            apiE2eMutexLockUnlock(true).catch(error => {
                console.error("Failed to release E2E mutex after read: " + error);
            });
        });
}


function storeE2EInfo(data) {
    apiE2eStore(data)
        .catch(error => {
            displayError(t("e2e.storeFailed") + error);
        });
}


function invalidCipherRedirectConfim() {
    if (confirm(t("e2e.invalidKey"))) {
        window.location = './e2eSetup';
    }
}

function isE2EKeySet() {
    let key = localStorage.getItem("e2ekey");
    return key !== null && key !== "";
}


async function loadWasm(func) {
    const go = new Go(); // Defined in wasm_exec.js
    try {
        const response = await fetch('e2e.wasm?v=1');
        if (!response.ok) throw new Error(t("download.initialiseFailed"));
        const obj = 'instantiateStreaming' in WebAssembly
            ? await WebAssembly.instantiateStreaming(response, go.importObject)
            : await WebAssembly.instantiate(await response.arrayBuffer(), go.importObject);
        go.run(obj.instance).catch(displayError);
        func();
    } catch (err) {
        displayError(err);
    }
}


function urlencodeFormData(fd) {
    let s = '';

    function encode(s) {
        return encodeURIComponent(s).replace(/%20/g, '+');
    }
    for (var pair of fd.entries()) {
        if (typeof pair[1] == 'string') {
            s += (s ? '&' : '') + encode(pair[0]) + '=' + encode(pair[1]);
        }
    }
    return s;
}


function setE2eUpload() {
    dropzoneObject.uploadFiles = function(files) {
        this._transformFiles(files, (transformedFiles) => {

            let transformedFile = transformedFiles[0];
            files[0].upload.chunked = true;
            files[0].isEndToEndEncrypted = true;

            let filename = files[0].upload.filename;
            let plainTextSize = transformedFile.size;
            let bytesSent = 0;

            let encryptedSize = GokapiE2EEncryptNew(files[0].upload.uuid, plainTextSize, filename);
            if (encryptedSize instanceof Error) {
                displayError(encryptedSize);
                return;
            }

            files[0].upload.totalChunkCount = Math.ceil(
                encryptedSize / this.options.chunkSize
            );

            files[0].sizeEncrypted = encryptedSize;
            let file = files[0];

            let bytesReadPlaintext = 0;
            let bytesSendEncrypted = 0;

            let finishedReading = false;
            let chunkIndex = 0;

            uploadChunk(file, 0, encryptedSize, plainTextSize, dropzoneObject.options.chunkSize, 0);

        });
    }
}


function decryptFileEntries(fileMap) {
    let datatable = $('#maintable').DataTable();
    const rows = datatable.rows().nodes();

    for (let i = 0; i < rows.length; i++) {
        const cell = datatable.cell(i, 0).node();
        const idAttr = $(cell).attr("id");
        if (!idAttr) continue;


        const id = idAttr.substring("cell-name-".length);
        const entry = fileMap[id];
        if (!entry) continue; // no matching file for this row

        const filename = entry[0];
        const cipher = entry[1];

        // Column 0: filename
        cell.textContent = filename;
        if (typeof decorateFileName === 'function') decorateFileName(cell);
        // Search and sorting must use the decrypted label, not the cached name.
        datatable.cell(i, 0).invalidate('dom');

        // Column 5: update link
        let urlNode = datatable.cell(i, 5).node();
        let urlLink = urlNode.querySelector("a");
        let url = urlLink.getAttribute("href");

        if (!url.includes(cipher)) {
            if (IncludeFilename) {
                url = url.replace("/Encrypted%20File", "/" + encodeURIComponent(filename));
            }
            url += "#" + cipher;
            urlLink.setAttribute("href", url);
        }

        // Column 6: update button and QR/email
        let buttonNode = datatable.cell(i, 6).node();
        let button = buttonNode.querySelector("button");
        button.setAttribute("data-clipboard-text", url);

        let qrElem = document.getElementById("qrcode-" + id);
        if (qrElem) qrElem.onclick = () => showQrCode(url);

        let emailElem = document.getElementById("email-" + id);
        if (emailElem) emailElem.href = "mailto:?body=" + encodeURIComponent(url);
    }
    datatable.draw(false);
}


async function uploadChunk(file, chunkIndex, encryptedTotalSize, plainTextSize, chunkSize, bytesWritten) {
    let isLastChunk = false;
    let bytesReadPlaintext = chunkIndex * chunkSize;
    let readEnd = bytesReadPlaintext + chunkSize;

    if (chunkIndex === file.upload.totalChunkCount - 1) {
        isLastChunk = true;
        readEnd = plainTextSize;
    }


    let dataBlock = file.webkitSlice ?
        file.webkitSlice(bytesReadPlaintext, readEnd) :
        file.slice(bytesReadPlaintext, readEnd);

    let data = await dataBlock.arrayBuffer();

    let dataEnc = await GokapiE2EUploadChunk(file.upload.uuid, data.byteLength, isLastChunk, new Uint8Array(data));
    if (dataEnc instanceof Error) {
        displayError(data);
        return;
    }
    let err = await postChunk(file.upload.uuid, bytesWritten, encryptedTotalSize, dataEnc, file);
    if (err !== null) {
        file.accepted = false;
        dropzoneObject._errorProcessing([file], err);
        return;
    }
    bytesWritten = bytesWritten + dataEnc.byteLength;
    data = null;
    dataEnc = null;
    dataBlock = null;

    if (!isLastChunk) {
        await uploadChunk(file, chunkIndex + 1, encryptedTotalSize, plainTextSize, chunkSize, bytesWritten)
    } else {
        file.status = Dropzone.SUCCESS;
        dropzoneObject.emit("success", file, 'success', null);
        dropzoneObject.emit("complete", file);
        dropzoneObject.processQueue();

        dropzoneObject.options.chunksUploaded(file, () => {});
    }
}

async function postChunk(uuid, bytesWritten, encSize, data, file) {
    return new Promise(resolve => {
        let formData = new FormData();
        formData.append("dztotalfilesize", encSize)
        formData.append("dzchunkbyteoffset", bytesWritten)
        formData.append("dzuuid", uuid)
        formData.append("file", new Blob([data]), "encrypted.file");

        let xhr = new XMLHttpRequest();
        xhr.open("POST", "./uploadChunk");

        let progressObj = xhr.upload != null ? xhr.upload : xhr;
        progressObj.onprogress = (event) => {
            try {
                dropzoneObject.emit("uploadprogress", file, (100 * (event.loaded + bytesWritten)) / encSize, event.loaded + bytesWritten);
            } catch (e) {
                console.log(e);
            }
        }
        xhr.onreadystatechange = function() {
            if (this.readyState == 4) {
                if (this.status == 200) {
                    resolve(null);
                } else {
                    console.log(xhr.responseText);
                    resolve(xhr.responseText);
                }
            }
        };
        xhr.send(formData);
    });
}
