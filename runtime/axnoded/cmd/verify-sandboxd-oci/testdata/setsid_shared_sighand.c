// Copyright 2026 Axern Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

#define _GNU_SOURCE

#include <sched.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/mman.h>
#include <sys/wait.h>
#include <unistd.h>

static void fail(const char *operation) {
  perror(operation);
  _exit(111);
}

static int idle_until_killed(void *unused) {
  (void)unused;
  for (;;) {
    pause();
  }
  return 0;
}

static void create_session_with_shared_sighand_sibling(void) {
  const size_t stack_size = 1U << 20;
  void *stack = mmap(NULL, stack_size, PROT_READ | PROT_WRITE,
                     MAP_PRIVATE | MAP_ANONYMOUS | MAP_STACK, -1, 0);
  if (stack == MAP_FAILED) {
    fail("mmap");
  }
  pid_t sibling = clone(idle_until_killed, (char *)stack + stack_size,
                        CLONE_VM | CLONE_SIGHAND | SIGCHLD, NULL);
  if (sibling < 0) {
    fail("clone");
  }
  if (setsid() < 0) {
    fail("setsid child");
  }
  if (kill(sibling, SIGKILL) < 0) {
    fail("kill sibling");
  }
  int status = 0;
  if (waitpid(sibling, &status, 0) != sibling) {
    fail("waitpid sibling");
  }
  if (!WIFSIGNALED(status) || WTERMSIG(status) != SIGKILL) {
    _exit(112);
  }
  _exit(0);
}

int main(void) {
  pid_t session_leader = fork();
  if (session_leader < 0) {
    fail("fork session leader");
  }
  if (session_leader == 0) {
    if (setsid() < 0) {
      fail("setsid leader");
    }
    pid_t child = fork();
    if (child < 0) {
      fail("fork child");
    }
    if (child == 0) {
      create_session_with_shared_sighand_sibling();
    }
    int status = 0;
    if (waitpid(child, &status, 0) != child) {
      fail("waitpid child");
    }
    if (!WIFEXITED(status) || WEXITSTATUS(status) != 0) {
      _exit(113);
    }
    _exit(0);
  }
  int status = 0;
  if (waitpid(session_leader, &status, 0) != session_leader) {
    fail("waitpid leader");
  }
  if (!WIFEXITED(status) || WEXITSTATUS(status) != 0) {
    return 114;
  }
  puts("setsid-shared-sighand-ok");
  return 0;
}
