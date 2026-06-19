// 未ログインの場合はどのボタンを押しても強制的にログイン画面へ
const isLoggedIn = document.body.dataset.login === "true";
document.querySelectorAll("a, button").forEach(el => {
  el.addEventListener("click", (e) => {
    if (!isLoggedIn) {
      e.preventDefault();
      window.location.href = "/login";
    }
  });
});

function showToast(message, type = "default") {
  const container = document.getElementById("toast-container");

  const toast = document.createElement("div");
  toast.className = `toast ${type}`;
  toast.innerText = message;

  container.appendChild(toast);

  // 表示アニメーション
  setTimeout(() => {
    toast.classList.add("show");
  }, 10);

  // 自動削除
  setTimeout(() => {
    toast.classList.remove("show");
    setTimeout(() => {
      toast.remove();
    }, 300);
  }, 3000);
}

function formatTime(date) {
  return date.toLocaleTimeString();
}

async function login() {
  window.location.href="/login"
}

async function start() {
  const date = document.getElementById("date").value
  const task_name = document.getElementById("task").value
  let select = document.getElementById("project_name");
  const project_name = select.options[select.selectedIndex].text;
  select = document.getElementById("work_class");
  const work_class = select.options[select.selectedIndex].text;

  currentStart = new Date();
  await endWork(date, formatTime(currentStart)) // 開始の後に開始を押したら、自動で終了するようにする
  await startWork(date, formatTime(currentStart), task_name, project_name, work_class)
}

function end() {
  const date = document.getElementById("date").value
  const task_name = document.getElementById("task").value
  currentStart = new Date();
  endWork(date, formatTime(currentStart))
}

function getWorks(date) {
  const url = new URL("/", window.location.origin);
  url.searchParams.set("date", date);
  window.location.href = url.toString();
}

async function editRow(button) {
  const row = button.closest("tr");

  for (let i = 0; i < 5; i++) {
    const cell = row.children[i];
    const currentValue = cell.innerText;
    if (i == 0 || i > 2) {
      cell.innerHTML = "";
      const input = document.createElement("input");
      input.type = "text";
      input.value = currentValue;
      input.style.width = "70%";
      cell.appendChild(input);
    } else if (i == 1) {
      cell.innerHTML = `
        <select class="project_name" onchange="updateWorkClassRow(this)"">
          <option ${currentValue === "" ? "selected" : ""}></option>
          <option value="idkiban" ${currentValue === "ID基盤開発・運用" ? "selected" : ""}>ID基盤開発・運用</option>
          <option value="umidasu" ${currentValue === "生み出す会議" ? "selected" : ""}>生み出す会議</option>
          <option value="sonota" ${currentValue === "その他" ? "selected" : ""}>その他</option>
        </select>
      `;
    } else if (i == 2) {
      cell.innerHTML = `
        <select class="work_class">
          <option disabled selected hidden>作業区分を選択</option>
        </select>
      `;
    }
  }
  const select = row.querySelector(".project_name");
  if (select) {
    updateWorkClassRow(select);
  }

  const actionCell = row.children[6];

  actionCell.innerHTML = `
    <button onclick="saveRow(this)">保存</button>
    <button onclick="deleteRow(this)">削除</button>
  `;
}

async function saveRow(button) {
  const row = button.closest("tr");
  const id = row.dataset.id;
  console.log(id)

  let newValues = []
  for (let i = 0; i < 5; i++) {
    if (i == 0 || i > 2) {
      const input = row.children[i].querySelector("input");
      newValues[i] = input.value;
      row.children[i].innerText = newValues[i];
    } else if (i == 1) {
      newValues[i] = row.querySelector(".project_name").options[row.querySelector(".project_name").selectedIndex].text
      row.children[i].innerText = newValues[i];
    } else if (i == 2) {
      newValues[i] = row.querySelector(".work_class")?.options?.[row.querySelector(".work_class")?.selectedIndex]?.text ?? "";
      row.children[i].innerText = newValues[i];
    }
  }
  
  await fetch(`http://localhost:3000/works/${id}`, {
    method: "PATCH",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      "task_name": newValues[0],
      "project_name": newValues[1],
      "work_class": newValues[2],
      "start_time": newValues[3],
      "end_time": newValues[4],
    })
  });

  const actionCell = row.children[6];
  actionCell.innerHTML = `<button onclick="editRow(this)">編集</button>`;
  getWorks(document.getElementById('date').value)
}

async function deleteRow(button) {
  const row = button.closest("tr");
  const id = row.dataset.id;
  
  await fetch(`http://localhost:3000/works/${id}`, {
    method: "DELETE",
    headers: {
      "Content-Type": "application/json",
    },
  });

  const actionCell = row.children[4];
  actionCell.innerHTML = `<button onclick="editRow(this)">編集</button>`;
  getWorks(document.getElementById('date').value)
}

async function startWork(date, time, task_name, project_name, work_class) {
  console.log(project_name, work_class)
  if (project_name === "プロジェクト名を選択")
    project_name = ""
  if (work_class === "作業区分を選択")
    work_class = ""
  try {
    const response = await fetch ("http://localhost:3000/works/start", {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        "date": date,
        "remarks": "今日の作業",
        "works": {
          "project_name": project_name,
          "work_class": work_class,
          "task_name": task_name,
          "start_time": `${date} ${time}`,
          "end_time": null,
          "memo": "実装"
        }
      })
    })
    if (!response.ok) {
      showToast(`HTTP Error: ${response.status}`, "error")
      return
    }
    getWorks(date)
  } catch {
    showToast('想定外のエラー', "error")
  }
}

async function endWork(date, time) {
  try {
    const response = await fetch ("http://localhost:3000/works/end", {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        "date": date,
        "remarks": "",
        "works": {
          "project_name": "",
          "work_class": "",
          "task_name": "",
          "start_time": null,
          "end_time": `${date} ${time}`,
          "memo": ""
        }
      })
    })
    if (!response.ok) {
      showToast(`HTTP Error: ${response.status}`, "error")
      return
    }
    getWorks(date)
  } catch {
    showToast("想定外のエラー", "error")
  }
}

async function lunch() {
  const date = document.getElementById("date").value
  await startWork(date, "11:45:00", "昼休み", "", "")
  await endWork(date, "12:45:00")
}

async function dailyScrum() {
  const date = document.getElementById("date").value
  await startWork(date, "11:00:00", "デイリースクラム", "ID基盤開発・運用", "開発作業")
  await endWork(date, "11:30:00")
}

async function afternoonAssembly() {
  const date = document.getElementById("date").value
  await endWork(date, "13:00:00") // 今作業中のものがあれば強制的に終わらせる
  await startWork(date, "13:00:00", "昼礼", "その他", "管理")
  await endWork(date, "13:15:00")
  getWorks(date)
}

document.getElementById("date").addEventListener("change", (e) => {
  const selectedDate = e.target.value;
  getWorks(selectedDate);
});


function updateWorkClass() {
  const parentValue = document.getElementById("project_name").value;
  const child = document.getElementById("work_class");

  // 一旦クリア
  child.innerHTML = "";
  let options = [];

  if (parentValue === "idkiban") {
    options = [
      { value: "", text: "開発作業" },
      { value: "", text: "運用作業" },
      { value: "", text: "保守作業" },
      { value: "", text: "先行検討_R&D" },
    ];
  } else if (parentValue === "umidasu") {
    options = [
      { value: "", text: "開発作業" },
      { value: "", text: "運用作業" },
      { value: "", text: "保守作業" },
      { value: "", text: "先行検討_R&D" },
    ];
  } else if (parentValue === "sonota") {
    options = [
      { value: "", text: "管理" },
      { value: "", text: "休暇" },
    ];
  }

  // option追加
  options.forEach(opt => {
    const option = document.createElement("option");
    option.value = opt.value;
    option.textContent = opt.text;
    child.appendChild(option);
  });
}

function updateWorkClassRow(el) {
  const row = el.closest("tr");
  const child = row.querySelector(".work_class");

  const value = el.value;

  child.innerHTML = "";

  let options = [];

  if (value === "idkiban") {
    options = [
      { value: "", text: "開発作業" },
      { value: "", text: "運用作業" },
      { value: "", text: "保守作業" },
      { value: "", text: "先行検討_R&D" },
    ];
  } else if (value === "umidasu") {
    options = [
      { value: "", text: "開発作業" },
      { value: "", text: "運用作業" },
      { value: "", text: "保守作業" },
      { value: "", text: "先行検討_R&D" },
    ];
  } else if (value === "sonota") {
    options = [
      { value: "", text: "管理" },
      { value: "", text: "休暇" },
    ];
  }

  options.forEach(opt => {
    const o = document.createElement("option");
    o.value = opt.value;
    o.textContent = opt.text;
    child.appendChild(o);
  });
}

function copyText() {
  const textarea = document.getElementById("copyText");

  if (navigator.clipboard) {
    navigator.clipboard.writeText(textarea.value);
  } else {
    textarea.select();
    document.execCommand("copy");
  }
}

// 行をクリックするとinputにコピーできる
function copyRow(row) {
  const cells = row.children;

  const task_name = cells[0].innerText;
  const project_name = cells[1].innerText;
  const work_class = cells[2].innerText;

  document.getElementById("task").value = task_name;
  
  const projectSelect = document.getElementById("project_name");
  const workSelect = document.getElementById("work_class");
  setSelectByText(projectSelect, project_name);
  setSelectByText(workSelect, work_class);
}

function setSelectByText(select, text) {
  for (let i = 0; i < select.options.length; i++) {
    if (select.options[i].text === text) {
      select.selectedIndex = i;
      break;
    }
  }

  select.dispatchEvent(new Event("change"));
}

function calcMinutes(start, end) {
  const toMin = (t) => {
    let [h, m] = t.split(":").map(Number);
    return h * 60 + m;
  };

  return toMin(end) - toMin(start);
}

function fillGaps() {
  let rows = document.querySelectorAll("tbody tr");
  let newRows = [];

  for (let i = 0; i < rows.length - 2; i++) {
    let current = rows[i].children;
    let next = rows[i + 1].children;

    let end = current[4].innerText;
    let nextStart = next[3].innerText;

    // 時間が違えば空きあり
    if (end && nextStart && end < nextStart) {
      let tr = document.createElement("tr");

      const values = ["事務作業", "その他", "管理", end, nextStart, String(calcMinutes(end, nextStart))];
      values.forEach((value) => {
        const td = document.createElement("td");
        td.textContent = value;
        tr.appendChild(td);
      });

      newRows.push({ index: i + 1, element: tr });
    }
  }

  newRows.reverse().forEach(item => {
    rows[item.index].parentNode.insertBefore(item.element, rows[item.index]);
  });

  appendGapToTextarea();
}

function appendGapToTextarea() {
  const textarea = document.getElementById("copyText");

  let text = textarea.value;
  let rows = document.querySelectorAll("tbody tr");

  let totalGapMinutes = 0;

  rows.forEach(row => {
    let cells = row.children;
    if (cells.length < 6) return;

    let task = cells[0].innerText;
    let minutes = parseInt(cells[5].innerText) || 0;

    if (task === "事務作業") {
      totalGapMinutes += minutes;
    }
  });

  if (totalGapMinutes === 0) return;

  let parts = text.split("・業務連絡");

  let before = parts[0];
  let after = "・業務連絡" + (parts[1] || "");

  before += `  ・[その他]<管理> 事務作業: ${totalGapMinutes}分\n`;

  textarea.value = before + after;
}

async function postToTeams() {
  const textarea = document.getElementById("copyText");
  let text = textarea.value;
  try {
    const response = await fetch ("http://localhost:3000/msgraph/teams", {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        "content": text
      })
    })
    if (!response.ok) {
      showToast(`HTTP Error: ${response.status}`, "error")
      return
    }
  } catch {
    showToast('想定外のエラー', "error")
  }
}