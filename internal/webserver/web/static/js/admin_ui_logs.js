// This file contains JS code for the Logs view
// All files named admin_*.js will be merged together and minimised by calling
// go generate ./...

function filterLogs(tag) {
    const textarea = document.getElementById('logviewer');
    if (tag == "all") {
        textarea.value = logContent;
    } else {
        textarea.value = logContent.split("\n").filter(line => line.includes("[" + tag + "]")).join("\n");
    }
    textarea.scrollTop = textarea.scrollHeight;
}

function setTrafficInfo(totalTraffic, recordingSince) {
    insertReadableSizeTwoOutputs(totalTraffic, 'totalTraffic', 'totalTrafficUnit');
    document.getElementById('cardTraffic').title= t("status.trafficSince", {date: formatUnixTimestamp(recordingSince)});
}

function setMemoryUsage(used, total) {
    insertReadableSizeTwoOutputs(total, 'totalMemory', 'memoryUnit');
    let unit = document.getElementById('memoryUnit').innerText;
    insertReadableSizeForcedUnit(used, 'usedMemory', unit);
}

function setDiskUsage(used, total) {
    insertReadableSizeTwoOutputs(total, 'totalDisk', 'diskUnit');
    let unit = document.getElementById('diskUnit').innerText;
    insertReadableSizeForcedUnit(used, 'usedDisk', unit);
}


function formatDuration(seconds) {
    const units = [{
            label: "y",
            value: 31536000
        },
        {
            label: "d",
            value: 86400
        },
        {
            label: "h",
            value: 3600
        },
        {
            label: "m",
            value: 60
        },
        {
            label: "s",
            value: 1
        },
    ];

    let startIndex = units.findIndex(unit => seconds >= unit.value);

    // If everything is below 1 minute, force start at minutes
    if (startIndex === -1 || units[startIndex].label === "s") {
        startIndex = units.findIndex(u => u.label === "m");
    }

    const first = units[startIndex];
    const second = units[startIndex + 1];

    const firstAmount = Math.floor(seconds / first.value);
    const remainder = seconds % first.value;
    const secondAmount = Math.floor(remainder / second.value);

    return `${firstAmount}${t("time." + first.label)} ${secondAmount}${t("time." + second.label)}`;
}

function addUptime() {
    if (currentUptime > 3600) {
        return;
    }
    setTimeout(() => {
        currentUptime = currentUptime + 1;
        document.getElementById('uptime').innerText = formatDuration(currentUptime);
        addUptime();
    }, 1000);
}

function setPercentageBar(id, num1, num2) {
    let percentage = num1;
    if (num2 !== undefined) {
        percentage = ((num1 / num2) * 100);
    }

    const bar = document.getElementById(id);

    bar.classList.remove("bg-success");
    bar.classList.remove("bg-warning");
    bar.classList.remove("bg-danger");

    if (percentage < 70) {
        bar.classList.add("bg-success");
    }
    if (percentage >= 70 && percentage < 90) {
        bar.classList.add("bg-warning");
    }
    if (percentage >= 90) {
        bar.classList.add("bg-danger");
    }
    bar.style.width = percentage + '%';
}


async function loadLogs(timestamp) {
    const textarea = document.getElementById('logviewer');

    try {
        const data = await apiLogGet(timestamp);
        lastLogUpdate = data.timestamp;
        let doScroll = true;
        if (timestamp != 0) {
            if (data.logEntries == "") {
                return;
            }
            doScroll = allowScroll();
            logContent = logContent + data.logEntries;
        } else {
            logContent = data.logEntries;
        }
        filterLogs(document.getElementById('logFilter').value);
        if (doScroll) {
            textarea.scrollTop = textarea.scrollHeight;
        }
    } catch (error) {
        lastLogUpdate = 0;
        console.error("Failed to load logs:", error);
        textarea.value = t("status.loadFailed");
    }
}


async function loadStatus() {
    try {
        const data = await apiLogSystemStatus();

        currentUptime = data.uptime;
        document.getElementById('labelCpu').innerText = data.cpuLoad + '%';
        document.getElementById('labelActiveFiles').innerText = data.activeFiles;
        setPercentageBar("barCpu", data.cpuLoad);
        setPercentageBar("barDisk", data.diskUsagePercentage);
        setPercentageBar("barMemory", data.memoryUsagePercentage);
        setMemoryUsage(data.memoryUsed, data.memoryTotal);
        setDiskUsage(data.diskUsed, data.diskTotal);
        setTrafficInfo(data.dataServed, data.trafficRecordingSince)
    } catch (error) {
        console.error("Failed to server status:", error);
    }
}

async function pollInfo() {
    firstStart = true;
    while (true) {
        await loadLogs(lastLogUpdate);
        if (firstStart) {
            firstStart = false;
        } else {
            await loadStatus();
        }
        await new Promise(r => setTimeout(r, POLL_INTERVAL_S * 1000));
    }
}

function allowScroll() {
    const textarea = document.getElementById('logviewer');
    return textarea.scrollTop + textarea.clientHeight >= textarea.scrollHeight - 5;
}


// Keep destructive actions in an explicit page dialog, with the shared busy/error behavior.
function showStatusAction(title, help, action, success, trigger, onClose) {
    const element = document.getElementById('statusActionModal');
    const confirm = document.getElementById('statusActionConfirm');
    document.getElementById('statusActionTitle').textContent = title;
    document.getElementById('statusActionHelp').textContent = help;
    document.getElementById('statusActionError').textContent = '';
    element.addEventListener('hidden.bs.modal', () => {
        confirm.onclick = null;
        if (onClose) onClose();
        if (trigger && trigger.isConnected) trigger.focus();
    }, {once: true});
    confirm.onclick = () => runModalSave('statusActionModal', confirm, async () => {
        await action();
        document.getElementById('statusActionResult').textContent = success;
    }, document.getElementById('statusActionError'));
    bootstrap.Modal.getOrCreateInstance(element).show();
}

function deleteLogs(trigger) {
    const selector = document.getElementById('deleteLogsSel');
    if (!selector) return;
    const cutoff = selector.value;
    const days = {"2": 2, "7": 7, "14": 14, "30": 30};
    if (cutoff !== 'all' && !Object.hasOwn(days, cutoff)) return;
    const timestamp = cutoff === 'all' ? 0 : Math.floor(Date.now() / 1000) - days[cutoff] * 86400;
    showStatusAction(t('status.deleteLogs'), t('status.confirmDelete'), async () => {
        await apiLogsDelete(timestamp);
        await loadLogs(0);
    }, t('status.logsDeleted'), trigger, () => { selector.value = 'none'; });
}

function resetTrafficStat(trigger) {
    showStatusAction(t('status.resetTraffic'), t('status.confirmReset'), async () => {
        await apiLogResetTraffic();
        await loadStatus();
    }, t('status.trafficReset'), trigger);
}
