# Baton todo creation

Create ordinary GitHub issues with titles and bodies that make sense to the
project even without Baton. Baton does not require headings, a form,
fingerprints, or policy comments.

For each independent outcome:

1. Write a durable problem/outcome description, relevant evidence,
   constraints, and observable acceptance criteria.
2. Create the issue through the project's normal GitHub workflow.
3. Choose the least-permissive mode: `trivial` for a small obvious change,
   `bounded` for clearly scoped implementation, or `investigate` for research
   and reporting.
4. Use `p2` unless the user or repository context supports another priority.
5. When the issue is ready, preview and then explicitly enroll the returned
   issue number with its classification, for example:

   ```sh
   baton --repo OWNER/REPOSITORY enroll ISSUE --mode bounded --priority p2 --dry-run
   baton --repo OWNER/REPOSITORY enroll ISSUE --mode bounded --priority p2
   ```

When the issue needs a blocker, do not enroll it as ready and add the blocker
later. Preview and apply `baton enroll ISSUE` without a mode first; the missing
mode keeps it blocked. Then preview and apply one `baton update ISSUE --mode
MODE --priority PRIORITY --add-blocker BLOCKER`. Never use body text or an old
eligibility label as enrollment authority. Report created issue numbers,
selected classification, and any issue left unenrolled pending a user
decision.
