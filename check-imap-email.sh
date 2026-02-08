#!/bin/bash
imapfilter -c /dev/stdin <<'EOF'
account = IMAP {
  server   = 'imap.gmail.com',
  username = 'delyanr@gmail.com',
  password = 'difq gxbc frnb oikz',
  ssl      = 'tls1'
}

inbox = account['INBOX']
uids = inbox:is_unseen()

local function expand_uids(set)
  local list = {}
  if type(set) ~= 'table' then
    return list
  end
  for _, entry in ipairs(set) do
    if type(entry) == 'table' then
      local start_uid = entry[1]
      local end_uid = entry[2] or entry[1]
      if type(start_uid) == 'number' and type(end_uid) == 'number' then
        for i = start_uid, end_uid do
          table.insert(list, i)
        end
      end
    elseif type(entry) == 'number' then
      table.insert(list, entry)
    end
  end
  return list
end

for _, uid in ipairs(expand_uids(uids)) do
  local msg = inbox[uid]
  local date    = msg:fetch_field('date') or ''
  local from    = msg:fetch_field('from') or ''
  local subject = msg:fetch_field('subject') or ''
  print(date .. ' | ' .. from .. ' | ' .. subject)
end
EOF
