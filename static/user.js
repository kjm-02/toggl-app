function editUser() {
  const fields = ["name", "nickname", "email"];

  fields.forEach((id) => {
    const el = document.getElementById(id);
    const value = el.innerText;
    el.innerHTML = `<input type="text" value="${value}" />`;
  });
  
  document.getElementById("editBtn").style.display = "none";
  document.getElementById("saveBtn").style.display = "inline-block"
}

async function saveUser() {
  const data = {
    name: document.querySelector("#name input").value,
    nickname: document.querySelector("#nickname input").value,
    email: document.querySelector("#email input").value,
  };
  const response = await fetch (`http://localhost:3000/user/update`, {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(data)
  })

  const fields = ["name", "nickname", "email"];
  fields.forEach((id) => {
    const value = document.querySelector(`#${id} input`).value;
    const el = document.getElementById(id);
    el.innerHTML = value; // ← span相当（テキスト表示）
  });


  document.getElementById("editBtn").style.display = "inline-block";
  document.getElementById("saveBtn").style.display = "none"
}