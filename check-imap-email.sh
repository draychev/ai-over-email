#!/bin/bash
imapfilter -c /dev/stdin <<'EOF'
options = options or {}
options.info = false

account = IMAP {
  server   = 'imap.gmail.com',
  username = 'delyanr@gmail.com',
  password = 'difq gxbc frnb oikz',
  ssl      = 'tls1'
}

inbox = account['INBOX']
uids = inbox:select_all()

local function expand_uids(set)
  local list = {}
  if type(set) ~= 'table' then
    return list
  end
  for _, entry in ipairs(set) do
    if type(entry) == 'table' then
      local first = entry[1]
      local second = entry[2]
      if type(second) == 'number' and type(first) == 'table' then
        table.insert(list, second)
      elseif type(first) == 'number' then
        local start_uid = first
        local end_uid = type(second) == 'number' and second or first
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

local all_uids = expand_uids(uids)
table.sort(all_uids)

local total = #all_uids
local count = total
if count > 50 then
  count = 50
end

for i = total, total - count + 1, -1 do
  local uid = all_uids[i]
  local msg = inbox[uid]
  local date    = msg:fetch_field('date') or ''
  local from    = msg:fetch_field('from') or ''
  local subject = msg:fetch_field('subject') or ''
  print(date .. ' | ' .. from .. ' | ' .. subject)
end
EOF
